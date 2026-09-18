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
	reviewStatus := constants.ReadingReviewNotRequired
	if risk == constants.RiskWarning || risk == constants.RiskCritical {
		reviewStatus = constants.ReadingReviewUnverified
	}
	reading := model.WaterReading{
		PondID: input.PondID, DissolvedOxygen: input.DissolvedOxygen, Temperature: input.Temperature,
		PH: input.PH, Ammonia: input.Ammonia, Turbidity: input.Turbidity, MeasuredAt: input.MeasuredAt.UTC(),
		Source: input.Source, RiskLevel: risk, AlertMessage: message, ReviewStatus: reviewStatus,
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

// Confirm 处理异常读数的第一级人工动作。
// 预警读数仍为单级确认：确认后立即生效；
// 严重读数只生成“待复核”记录，读数不生效，需另一名操作员终审。
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
	before := reading
	now := time.Now().UTC()
	if err := applyVerify(&reading, note, actor, now); err != nil {
		return model.WaterReading{}, err
	}
	if err := s.repo.Save(&reading); err != nil {
		return model.WaterReading{}, WrapError(CodeInternal, "确认异常读数失败", err)
	}
	if reading.RiskLevel == constants.RiskCritical {
		if err := s.audit.Record(actor, "verify", "water_reading", reading.ID, before, reading, "首名操作员现场核实严重读数，提交待复核："+note); err != nil {
			return model.WaterReading{}, err
		}
	} else if err := s.audit.Record(actor, "confirm", "water_reading", reading.ID, before, reading, note); err != nil {
		return model.WaterReading{}, err
	}
	return reading, nil
}

// ReviewCritical 是严重读数双人复核闭环的第二级终审，必须由首名核实人之外的操作员执行。
// 通过后读数才生效（Confirmed）；否决则读数保持严重且不生效，并记录否决原因。
func (s *ReadingService) ReviewCritical(id uint, approved bool, note string, actor Actor) (model.WaterReading, error) {
	if !s.transactional {
		var result model.WaterReading
		err := s.withinTransaction(func(scoped *ReadingService) error {
			var inner error
			result, inner = scoped.ReviewCritical(id, approved, note, actor)
			return inner
		})
		return result, err
	}
	reading, err := s.Get(id)
	if err != nil {
		return model.WaterReading{}, err
	}
	before := reading
	now := time.Now().UTC()
	if err := applyCriticalReview(&reading, approved, note, actor, now); err != nil {
		return model.WaterReading{}, err
	}
	if err := s.repo.Save(&reading); err != nil {
		return model.WaterReading{}, WrapError(CodeInternal, "复核严重读数失败", err)
	}
	reason := strings.TrimSpace(note)
	if approved {
		if reason == "" {
			reason = "第二名操作员复核通过，严重读数生效"
		} else {
			reason = "第二名操作员复核通过：" + reason
		}
		if err := s.audit.Record(actor, "review_approve", "water_reading", reading.ID, before, reading, reason); err != nil {
			return model.WaterReading{}, err
		}
	} else {
		if err := s.audit.Record(actor, "review_reject", "water_reading", reading.ID, before, reading, "第二名操作员复核否决："+reason); err != nil {
			return model.WaterReading{}, err
		}
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
	if reading.ReviewStatus == constants.ReadingReviewPendingReview || reading.ReviewStatus == constants.ReadingReviewRejected {
		return NewError(CodeConflict, "读数已进入严重异常双人复核流程，不能删除")
	}
	if err := s.repo.Delete(&reading); err != nil {
		return WrapError(CodeInternal, "删除水质读数失败", err)
	}
	return s.audit.Record(actor, "delete", "water_reading", reading.ID, reading, nil, "删除手工录入读数")
}

// ReadingBlockMessage 返回读数尚不能作为放行依据时的阻断原因；返回空串表示读数已生效可放行。
// 正常读数天然生效；预警读数需单级确认；严重读数必须由两名不同操作员完成复核并通过。
// 待复核或被否决的严重读数会阻断计划批准与投喂安排，避免系统退回更早的读数放行。
func ReadingBlockMessage(reading model.WaterReading) string {
	switch reading.RiskLevel {
	case constants.RiskNormal:
		return ""
	case constants.RiskWarning:
		if reading.Confirmed {
			return ""
		}
		return "最新水质预警尚未完成确认，不能放行"
	case constants.RiskCritical:
		if reading.Confirmed {
			return ""
		}
		switch reading.ReviewStatus {
		case constants.ReadingReviewPendingReview:
			return "严重读数正在等待第二名操作员复核，复核完成前不能放行"
		case constants.ReadingReviewRejected:
			return "严重读数已被复核否决，水质仍为严重异常，不能放行"
		default:
			return "严重读数尚未完成两名操作员复核，不能放行"
		}
	}
	return "最新水质读数状态异常，不能放行"
}

func actorName(actor Actor) string {
	if strings.TrimSpace(actor.DisplayName) != "" {
		return actor.DisplayName
	}
	return actor.Username
}

// applyVerify 执行第一级人工动作的状态迁移，便于脱离数据库进行单测。
func applyVerify(reading *model.WaterReading, note string, actor Actor, now time.Time) error {
	switch reading.RiskLevel {
	case constants.RiskNormal:
		return NewError(CodeConflict, "正常读数无需异常确认")
	case constants.RiskWarning:
		if reading.Confirmed || reading.ReviewStatus == constants.ReadingReviewApproved {
			return NewError(CodeConflict, "该异常读数已确认")
		}
		if reading.ReviewStatus != constants.ReadingReviewUnverified {
			return NewError(CodeConflict, "预警读数当前状态不允许确认")
		}
		name := actorName(actor)
		reading.ReviewStatus = constants.ReadingReviewApproved
		reading.Confirmed = true
		reading.ConfirmedBy = name
		reading.ConfirmedAt = &now
		reading.ConfirmationNote = strings.TrimSpace(note)
		reading.VerifiedByUserID = actor.UserID
		reading.VerifiedBy = name
		reading.VerifiedAt = &now
		return nil
	case constants.RiskCritical:
		if reading.Confirmed || reading.ReviewStatus == constants.ReadingReviewApproved {
			return NewError(CodeConflict, "该严重读数已完成双人复核")
		}
		if reading.ReviewStatus == constants.ReadingReviewPendingReview {
			return NewError(CodeConflict, "该严重读数已提交待复核，等待第二名操作员终审")
		}
		if reading.ReviewStatus != constants.ReadingReviewUnverified && reading.ReviewStatus != constants.ReadingReviewRejected {
			return NewError(CodeConflict, "严重读数当前状态不允许核实")
		}
		// 首次核实或否决后重新核实：只生成/刷新待复核记录，读数不生效。
		reading.ReviewStatus = constants.ReadingReviewPendingReview
		reading.VerifiedByUserID = actor.UserID
		reading.VerifiedBy = actorName(actor)
		reading.VerifiedAt = &now
		reading.ConfirmationNote = strings.TrimSpace(note)
		// 重新核实时清空上一轮终审痕迹，保证闭环最终只保留一个有效结果。
		reading.ReviewByUserID = 0
		reading.ReviewBy = ""
		reading.ReviewAt = nil
		reading.ReviewNote = ""
		return nil
	}
	return NewError(CodeValidation, "未知的风险等级")
}

// applyCriticalReview 执行第二级终审的状态迁移，便于脱离数据库进行单测。
func applyCriticalReview(reading *model.WaterReading, approved bool, note string, actor Actor, now time.Time) error {
	if reading.RiskLevel != constants.RiskCritical {
		return NewError(CodeConflict, "只有严重读数需要双人复核")
	}
	if reading.Confirmed || reading.ReviewStatus == constants.ReadingReviewApproved {
		return NewError(CodeConflict, "该严重读数已完成双人复核")
	}
	if reading.ReviewStatus != constants.ReadingReviewPendingReview {
		return NewError(CodeConflict, "该严重读数尚未进入待复核状态，需先由首名操作员核实")
	}
	if reading.VerifiedByUserID != 0 && actor.UserID != 0 && reading.VerifiedByUserID == actor.UserID {
		return NewError(CodeConflict, "第二名复核人必须与首名核实人不同")
	}
	trimmed := strings.TrimSpace(note)
	if !approved && len([]rune(trimmed)) < 2 {
		return NewError(CodeValidation, "否决严重读数必须写明原因")
	}
	reading.ReviewByUserID = actor.UserID
	reading.ReviewBy = actorName(actor)
	reading.ReviewAt = &now
	if approved {
		reading.ReviewStatus = constants.ReadingReviewApproved
		reading.Confirmed = true
		reading.ConfirmedBy = actorName(actor)
		reading.ConfirmedAt = &now
		if trimmed == "" {
			reading.ReviewNote = "复核通过"
		} else {
			reading.ReviewNote = trimmed
		}
	} else {
		reading.ReviewStatus = constants.ReadingReviewRejected
		reading.ReviewNote = trimmed
	}
	return nil
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
