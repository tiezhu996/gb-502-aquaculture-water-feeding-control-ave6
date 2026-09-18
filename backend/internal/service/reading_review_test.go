package service

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"aquaculture-water-feeding-control/backend/internal/model"
	"strings"
	"testing"
	"time"
)

func operatorActor(id uint, name string) Actor {
	return Actor{UserID: id, Username: name, DisplayName: name, Role: string(constants.RoleOperator)}
}

func newReading(risk constants.RiskLevel) model.WaterReading {
	status := constants.ReadingReviewNotRequired
	if risk == constants.RiskWarning || risk == constants.RiskCritical {
		status = constants.ReadingReviewUnverified
	}
	return model.WaterReading{RiskLevel: risk, ReviewStatus: status}
}

// 预警读数保持单级确认：一次确认即生效。
func TestWarningSingleLevelConfirmUnchanged(t *testing.T) {
	reading := newReading(constants.RiskWarning)
	now := time.Now().UTC()
	if err := applyVerify(&reading, "现场复核指标偏高", operatorActor(1, "操作员甲"), now); err != nil {
		t.Fatalf("warning verify failed: %v", err)
	}
	if !reading.Confirmed || reading.ReviewStatus != constants.ReadingReviewApproved {
		t.Fatalf("warning should be confirmed after one step, got status=%s confirmed=%v", reading.ReviewStatus, reading.Confirmed)
	}
	if err := applyVerify(&reading, "再次确认", operatorActor(2, "操作员乙"), now); err == nil {
		t.Fatal("duplicated warning confirmation must be rejected")
	}
	if ReadingBlockMessage(reading) != "" {
		t.Fatal("confirmed warning reading should be releasable")
	}
}

// 正常读数不需要确认，天然可放行。
func TestNormalReadingRequiresNoReview(t *testing.T) {
	reading := newReading(constants.RiskNormal)
	if err := applyVerify(&reading, "x", operatorActor(1, "操作员甲"), time.Now().UTC()); err == nil {
		t.Fatal("normal reading must not accept confirmation")
	}
	if ReadingBlockMessage(reading) != "" {
		t.Fatal("normal reading should be releasable")
	}
}

// 严重读数首名操作员核实后只进入待复核，读数不生效且阻断放行。
func TestCriticalVerifyOnlyCreatesPendingReview(t *testing.T) {
	reading := newReading(constants.RiskCritical)
	now := time.Now().UTC()
	first := operatorActor(1, "操作员甲")
	if err := applyVerify(&reading, "现场仪表复测仍严重", first, now); err != nil {
		t.Fatalf("critical verify failed: %v", err)
	}
	if reading.ReviewStatus != constants.ReadingReviewPendingReview {
		t.Fatalf("status = %s, want pending_review", reading.ReviewStatus)
	}
	if reading.Confirmed {
		t.Fatal("critical reading must not take effect after first-level verification")
	}
	if reading.VerifiedByUserID != first.UserID {
		t.Fatal("first verifier must be recorded")
	}
	if ReadingBlockMessage(reading) == "" {
		t.Fatal("pending critical reading must block plan approval and feeding")
	}
	// 重复核实必须被拒绝，闭环只能有一个在途结果。
	if err := applyVerify(&reading, "重复核实", first, now); err == nil {
		t.Fatal("duplicated verification while pending must be rejected")
	}
	// 未经过第二级终审的状态也不能直接终审。
	if err := applyCriticalReview(&reading, true, "", first, now); err == nil {
		t.Fatal("same operator must not complete both levels")
	}
}

// 两名不同操作员：通过后读数才生效，重复终审只能产生一个结果。
func TestCriticalTwoPersonApprove(t *testing.T) {
	reading := newReading(constants.RiskCritical)
	now := time.Now().UTC()
	first := operatorActor(1, "操作员甲")
	second := operatorActor(2, "操作员乙")
	if err := applyVerify(&reading, "现场核实", first, now); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if err := applyCriticalReview(&reading, true, "复核数据一致", second, now); err != nil {
		t.Fatalf("second operator approve failed: %v", err)
	}
	if !reading.Confirmed || reading.ReviewStatus != constants.ReadingReviewApproved {
		t.Fatalf("critical reading should be effective after approval, got %s", reading.ReviewStatus)
	}
	if reading.ConfirmedBy != "操作员乙" {
		t.Fatalf("confirmedBy = %q, want 操作员乙", reading.ConfirmedBy)
	}
	if ReadingBlockMessage(reading) != "" {
		t.Fatal("approved critical reading should be releasable")
	}
	// 并发/重复终审：已完成闭环后任何终审都必须失败。
	if err := applyCriticalReview(&reading, false, "晚到的否决", first, now); err == nil {
		t.Fatal("late review after closure must be rejected")
	}
	if err := applyCriticalReview(&reading, true, "重复通过", second, now); err == nil {
		t.Fatal("duplicated review must produce no second result")
	}
}

// 否决必须写明原因；读数保持严重并继续阻断，重新核实后可再次进入待复核。
func TestCriticalRejectKeepsSevereThenReverify(t *testing.T) {
	reading := newReading(constants.RiskCritical)
	now := time.Now().UTC()
	first := operatorActor(1, "操作员甲")
	second := operatorActor(2, "操作员乙")
	if err := applyVerify(&reading, "现场核实", first, now); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if err := applyCriticalReview(&reading, false, "   ", second, now); err == nil {
		t.Fatal("rejection without reason must be rejected")
	}
	reason := "复核发现取样受底泥污染，读数不具代表性，维持严重"
	if err := applyCriticalReview(&reading, false, reason, second, now); err != nil {
		t.Fatalf("reject failed: %v", err)
	}
	if reading.Confirmed {
		t.Fatal("rejected critical reading must not be confirmed")
	}
	if reading.ReviewStatus != constants.ReadingReviewRejected {
		t.Fatalf("status = %s, want rejected", reading.ReviewStatus)
	}
	if !strings.Contains(reading.ReviewNote, "底泥污染") {
		t.Fatalf("reject reason not recorded: %q", reading.ReviewNote)
	}
	if ReadingBlockMessage(reading) == "" {
		t.Fatal("rejected critical reading must keep blocking release")
	}
	// 否决后首名操作员可重新核实，再次进入待复核，上一轮终审痕迹被清空。
	if err := applyVerify(&reading, "重新取样后再次核实", first, now.Add(time.Minute)); err != nil {
		t.Fatalf("re-verify after rejection failed: %v", err)
	}
	if reading.ReviewStatus != constants.ReadingReviewPendingReview || reading.Confirmed {
		t.Fatalf("re-verification should reopen pending review, got %s", reading.ReviewStatus)
	}
	if reading.ReviewByUserID != 0 || reading.ReviewNote != "" {
		t.Fatal("previous terminal review traces must be cleared on re-verification")
	}
	// 仍须由另一名操作员终审；同一人依旧不能完成两级。
	if err := applyCriticalReview(&reading, true, "", first, now.Add(2*time.Minute)); err == nil {
		t.Fatal("same operator must not complete both levels after re-verification")
	}
	third := operatorActor(3, "操作员丙")
	if err := applyCriticalReview(&reading, true, "新样品复核通过", third, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("second-level approval by another operator failed: %v", err)
	}
	if !reading.Confirmed || reading.ReviewStatus != constants.ReadingReviewApproved {
		t.Fatalf("reading should be effective, got %s", reading.ReviewStatus)
	}
}

// 尚未进入待复核的严重读数不能直接终审。
func TestCriticalReviewRequiresPendingState(t *testing.T) {
	reading := newReading(constants.RiskCritical)
	if err := applyCriticalReview(&reading, true, "", operatorActor(2, "操作员乙"), time.Now().UTC()); err == nil {
		t.Fatal("terminal review before first-level verification must be rejected")
	}
}
