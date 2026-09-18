package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/dto"
	"aquaculture-water-feeding-control/backend/internal/model"
	"aquaculture-water-feeding-control/backend/internal/repository"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type ReadingService struct {
	repo          *repository.ReadingRepository
	ponds         *repository.PondRepository
	audit         *AuditService
	transactional bool
}

func (s *ReadingService) withinTransaction(fn func(*ReadingService) error) error {
	return s.audit.WithinTransaction(func(tx *gorm.DB, audit *AuditService) error {
		scoped := &ReadingService{repo: repository.NewReadingRepository(tx), ponds: repository.NewPondRepository(tx), audit: audit, transactional: true}
		return fn(scoped)
	})
}

func NewReadingService(repo *repository.ReadingRepository, ponds *repository.PondRepository, audit *AuditService) *ReadingService {
	return &ReadingService{repo: repo, ponds: ponds, audit: audit}
}

func (s *ReadingService) List(query dto.PageQuery, pondID uint, unconfirmed bool) (dto.PageResult[model.WaterReading], error) {
	query.Normalize()
	items, total, err := s.repo.List(query, pondID, unconfirmed)
	if err != nil {
		return dto.PageResult[model.WaterReading]{}, WrapError(CodeInternal, "查询水质读数失败", err)
	}
	return dto.PageResult[model.WaterReading]{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func (s *ReadingService) Get(id uint) (model.WaterReading, error) {
	var reading model.WaterReading
	var err error
	if s.transactional {
		reading, err = s.repo.GetForUpdate(id)
	} else {
		reading, err = s.repo.Get(id)
	}
	if err == gorm.ErrRecordNotFound {
		return model.WaterReading{}, NewError(CodeNotFound, "水质读数不存在")
	}
	if err != nil {
		return model.WaterReading{}, WrapError(CodeInternal, "查询水质读数失败", err)
	}
	return reading, nil
}

func (s *ReadingService) Create(input dto.WaterReadingInput, actor Actor) (model.WaterReading, error) {
	if !s.transactional {
		var result model.WaterReading
		err := s.withinTransaction(func(scoped *ReadingService) error {
			var inner error
			result, inner = scoped.Create(input, actor)
			return inner
		})
		return result, err
	}
	pond, err := s.ponds.GetForUpdate(input.PondID)
	if err == gorm.ErrRecordNotFound {
		return model.WaterReading{}, NewError(CodeValidation, "养殖池不存在")
	}
	if err != nil {
		return model.WaterReading{}, WrapError(CodeInternal, "查询养殖池失败", err)
	}
	if pond.Status == constants.PondStatusClosed {
		return model.WaterReading{}, NewError(CodeConflict, "已关闭养殖池不能新增读数")
	}
	if input.MeasuredAt.After(time.Now().Add(10 * time.Minute)) {
		return model.WaterReading{}, NewError(CodeValidation, "测量时间不能晚于当前时间")
	}
	risk, message := assessWaterRisk(input)
	reading := model.WaterReading{
		PondID: input.PondID, DissolvedOxygen: input.DissolvedOxygen, Temperature: input.Temperature,
		PH: input.PH, Ammonia: input.Ammonia, Turbidity: input.Turbidity, MeasuredAt: input.MeasuredAt.UTC(),
		Source: input.Source, RiskLevel: risk, AlertMessage: message,
	}
	if err := s.repo.Create(&reading); err != nil {
		return model.WaterReading{}, WrapError(CodeInternal, "创建水质读数失败", err)
	}
	reading.Pond = &pond
	if err := s.audit.Record(actor, "create", "water_reading", reading.ID, nil, reading, message); err != nil {
		return model.WaterReading{}, err
	}
	return reading, nil
}

// Confirm 对预警读数执行原有单级确认；对严重读数则只完成首名操作员的一级核实，
// 生成“待复核”记录，不解除异常，也不能据此批准计划或安排投喂。
func (s *ReadingService) Confirm(id uint, note string, actor Actor) (model.WaterReading, error) {
	if !s.transactional {
		var result model.WaterReading
		err := s.withinTransaction(func(scoped *ReadingService) error {
			var inner error
			result, inner = scoped.Confirm(id, note, actor)
			return inner
		})
		return result, err
	}
	reading, err := s.Get(id)
	if err != nil {
		return model.WaterReading{}, err
	}
	if err := validateFirstVerification(reading, actor.UserID); err != nil {
		return model.WaterReading{}, err
	}
	before := reading
	trimmed := strings.TrimSpace(note)
	now := time.Now().UTC()
	if reading.RiskLevel == constants.RiskWarning {
		reading.Confirmed = true
		reading.ConfirmedBy = actorName(actor)
		reading.ConfirmedAt = &now
		reading.ConfirmationNote = trimmed
		if err := s.repo.Save(&reading); err != nil {
			return model.WaterReading{}, WrapError(CodeInternal, "确认异常读数失败", err)
		}
		if err := s.audit.Record(actor, "confirm", "water_reading", reading.ID, before, reading, note); err != nil {
			return model.WaterReading{}, err
		}
		return reading, nil
	}
	// 严重读数：一级核实只生成待复核记录，Confirmed 仍为 false。
	reading.ReviewStatus = constants.ReviewPending
	reading.VerifiedByUserID = actor.UserID
	reading.VerifiedBy = actorName(actor)
	reading.VerifiedAt = &now
	reading.VerificationNote = trimmed
	// 若为否决后重新发起核实，清空上一轮二级复核结果，保证新一轮只有一个结果。
	reading.ReviewedByUserID = 0
	reading.ReviewedBy = ""
	reading.ReviewedAt = nil
	reading.ReviewNote = ""
	reading.RejectionReason = ""
	if err := s.repo.Save(&reading); err != nil {
		return model.WaterReading{}, WrapError(CodeInternal, "提交严重读数核实失败", err)
	}
	if err := s.audit.Record(actor, "verify_critical", "water_reading", reading.ID, before, reading, note); err != nil {
		return model.WaterReading{}, err
	}
	return reading, nil
}

// ApproveReview 由另一名操作员通过二级复核，严重读数自此生效。
// 重复或并发复核只会产生一个结果：状态不是待复核时一律拒绝。
func (s *ReadingService) ApproveReview(id uint, note string, actor Actor) (model.WaterReading, error) {
	return s.secondReview(id, strings.TrimSpace(note), actor, true)
}

// RejectReview 由另一名操作员否决二级复核，读数保持严重并写明原因，
// 现场处置后可重新发起一级核实，再由不同的人复核。
func (s *ReadingService) RejectReview(id uint, reason string, actor Actor) (model.WaterReading, error) {
	return s.secondReview(id, strings.TrimSpace(reason), actor, false)
}

func (s *ReadingService) secondReview(id uint, note string, actor Actor, approved bool) (model.WaterReading, error) {
	if !s.transactional {
		var result model.WaterReading
		err := s.withinTransaction(func(scoped *ReadingService) error {
			var inner error
			result, inner = scoped.secondReview(id, note, actor, approved)
			return inner
		})
		return result, err
	}
	reading, err := s.Get(id)
	if err != nil {
		return model.WaterReading{}, err
	}
	if reading.RiskLevel != constants.RiskCritical {
		return model.WaterReading{}, NewError(CodeConflict, "只有严重读数需要双人复核")
	}
	if err := validateSecondReview(reading, actor.UserID); err != nil {
		return model.WaterReading{}, err
	}
	before := reading
	now := time.Now().UTC()
	reading.ReviewedByUserID = actor.UserID
	reading.ReviewedBy = actorName(actor)
	reading.ReviewedAt = &now
	reading.ReviewNote = note
	action := "review_approve"
	if approved {
		reading.ReviewStatus = constants.ReviewApproved
		reading.RejectionReason = ""
		reading.Confirmed = true
		reading.ConfirmedBy = actorName(actor)
		reading.ConfirmedAt = &now
		reading.ConfirmationNote = note
	} else {
		reading.ReviewStatus = constants.ReviewRejected
		reading.RejectionReason = note
		// 否决：保持严重、保持未确认，异常未解除。
		reading.Confirmed = false
		reading.ConfirmedBy = ""
		reading.ConfirmedAt = nil
		reading.ConfirmationNote = ""
		action = "review_reject"
	}
	if err := s.repo.Save(&reading); err != nil {
		return model.WaterReading{}, WrapError(CodeInternal, "提交严重读数复核结果失败", err)
	}
	if err := s.audit.Record(actor, action, "water_reading", reading.ID, before, reading, note); err != nil {
		return model.WaterReading{}, err
	}
	return reading, nil
}

func (s *ReadingService) Delete(id uint, actor Actor) error {
	if !s.transactional {
		return s.withinTransaction(func(scoped *ReadingService) error { return scoped.Delete(id, actor) })
	}
	reading, err := s.Get(id)
	if err != nil {
		return err
	}
	if reading.Source != "manual" {
		return NewError(CodeConflict, "只允许删除手工录入的读数")
	}
	if reading.Confirmed {
		return NewError(CodeConflict, "已确认的异常读数不能删除")
	}
	if reading.ReviewStatus == constants.ReviewPending {
		return NewError(CodeConflict, "严重读数正在等待二级复核，不能删除")
	}
	if err := s.repo.Delete(&reading); err != nil {
		return WrapError(CodeInternal, "删除水质读数失败", err)
	}
	return s.audit.Record(actor, "delete", "water_reading", reading.ID, reading, nil, "删除手工录入读数")
}

// validateFirstVerification 校验一级入口（预警确认 / 严重核实）是否合法。
func validateFirstVerification(reading model.WaterReading, actorUserID uint) error {
	if reading.RiskLevel == constants.RiskNormal {
		return NewError(CodeConflict, "正常读数无需异常确认")
	}
	if reading.RiskLevel == constants.RiskWarning {
		if reading.Confirmed {
			return NewError(CodeConflict, "该异常读数已确认")
		}
		return nil
	}
	switch reading.ReviewStatus {
	case constants.ReviewPending:
		return NewError(CodeConflict, "该严重读数已完成一级核实，正在等待另一名操作员复核")
	case constants.ReviewApproved:
		return NewError(CodeConflict, "该严重读数已通过双人复核，结果只能产生一次")
	case constants.ReviewRejected:
		// 否决后允许重新发起一级核实，开启下一轮双人复核。
		return nil
	default:
		return nil
	}
}

// validateSecondReview 校验二级复核入口：只有待复核记录可被复核（重复/并发只能产生一个结果），
// 且必须由首名核实人之外的另一名操作员完成。
func validateSecondReview(reading model.WaterReading, actorUserID uint) error {
	if reading.ReviewStatus != constants.ReviewPending {
		return NewError(CodeConflict, "该严重读数当前不处于待复核状态，复核结果只能产生一次")
	}
	if actorUserID == 0 || reading.VerifiedByUserID == 0 || actorUserID == reading.VerifiedByUserID {
		return NewError(CodeConflict, "二级复核必须由首名核实人之外的另一名操作员完成")
	}
	return nil
}

// criticalReadingsBlock 判定严重读数在计划批准/投喂安排环节的阻断文案。
// 待复核与被否决均保持严重，不能通过退回更早读数放行。
func criticalReadingsBlock(reviewStatus constants.ReadingReviewStatus) string {
	switch reviewStatus {
	case constants.ReviewPending:
		return "最新严重读数正在等待另一名操作员复核，不能退回更早读数放行，也不能放行投喂"
	case constants.ReviewRejected:
		return "最新严重读数复核已被否决，水质仍为严重，不能放行投喂"
	default:
		return "存在严重水质异常，不能放行投喂"
	}
}

func actorName(actor Actor) string {
	if actor.DisplayName != "" {
		return actor.DisplayName
	}
	return actor.Username
}

func assessWaterRisk(input dto.WaterReadingInput) (constants.RiskLevel, string) {
	critical := make([]string, 0)
	warnings := make([]string, 0)
	if input.DissolvedOxygen < 3 {
		critical = append(critical, fmt.Sprintf("溶解氧 %.1f mg/L 严重偏低", input.DissolvedOxygen))
	} else if input.DissolvedOxygen < 5 {
		warnings = append(warnings, fmt.Sprintf("溶解氧 %.1f mg/L 偏低", input.DissolvedOxygen))
	}
	if input.PH < 5.5 || input.PH > 10 {
		critical = append(critical, fmt.Sprintf("pH %.1f 超出安全范围", input.PH))
	} else if input.PH < 6.5 || input.PH > 9 {
		warnings = append(warnings, fmt.Sprintf("pH %.1f 偏离建议范围", input.PH))
	}
	if input.Ammonia > 1 {
		critical = append(critical, fmt.Sprintf("氨氮 %.2f mg/L 严重超标", input.Ammonia))
	} else if input.Ammonia > 0.3 {
		warnings = append(warnings, fmt.Sprintf("氨氮 %.2f mg/L 偏高", input.Ammonia))
	}
	if input.Temperature < 10 || input.Temperature > 36 {
		critical = append(critical, fmt.Sprintf("水温 %.1f℃ 超出安全范围", input.Temperature))
	} else if input.Temperature < 18 || input.Temperature > 32 {
		warnings = append(warnings, fmt.Sprintf("水温 %.1f℃ 需关注", input.Temperature))
	}
	if input.Turbidity > 100 {
		warnings = append(warnings, fmt.Sprintf("浊度 %.0f NTU 偏高", input.Turbidity))
	}
	if len(critical) > 0 {
		return constants.RiskCritical, strings.Join(append(critical, warnings...), "；")
	}
	if len(warnings) > 0 {
		return constants.RiskWarning, strings.Join(warnings, "；")
	}
	return constants.RiskNormal, "各项水质指标在控制范围内"
}
