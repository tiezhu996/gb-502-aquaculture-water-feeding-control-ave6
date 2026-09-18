package constants

type PondStatus string

const (
	PondStatusActive     PondStatus = "active"
	PondStatusQuarantine PondStatus = "quarantine"
	PondStatusClosed     PondStatus = "closed"
)

func (s ExecutionStatus) Valid() bool {
	return s == ExecutionScheduled || s == ExecutionRunning || s == ExecutionCompleted || s == ExecutionCancelled
}

func (s ExecutionStatus) CanTransitionTo(next ExecutionStatus) bool {
	switch s {
	case ExecutionScheduled:
		return next == ExecutionScheduled || next == ExecutionRunning || next == ExecutionCompleted || next == ExecutionCancelled
	case ExecutionRunning:
		return next == ExecutionRunning || next == ExecutionCompleted || next == ExecutionCancelled
	default:
		return false
	}
}

func (s PondStatus) Valid() bool {
	return s == PondStatusActive || s == PondStatusQuarantine || s == PondStatusClosed
}

type PlanStatus string

const (
	PlanStatusDraft    PlanStatus = "draft"
	PlanStatusPending  PlanStatus = "pending"
	PlanStatusApproved PlanStatus = "approved"
	PlanStatusExecuted PlanStatus = "executed"
)

func (s PlanStatus) Valid() bool {
	return s == PlanStatusDraft || s == PlanStatusPending || s == PlanStatusApproved || s == PlanStatusExecuted
}

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleManager  Role = "manager"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleManager || r == RoleOperator || r == RoleViewer
}

type RiskLevel string

const (
	RiskNormal   RiskLevel = "normal"
	RiskWarning  RiskLevel = "warning"
	RiskCritical RiskLevel = "critical"
)

// ReadingReview 表示水质读数（尤其是严重读数）的人工复核闭环状态。
// 正常读数为 not_required；预警读数保持单级确认 unverified -> approved；
// 严重读数必须双人复核：unverified -> pending_review -> approved / rejected，
// rejected 后允许首名操作员重新核实，再次进入 pending_review。
type ReadingReview string

const (
	ReadingReviewNotRequired   ReadingReview = "not_required"
	ReadingReviewUnverified    ReadingReview = "unverified"
	ReadingReviewPendingReview ReadingReview = "pending_review"
	ReadingReviewApproved      ReadingReview = "approved"
	ReadingReviewRejected      ReadingReview = "rejected"
)

func (s ReadingReview) Valid() bool {
	switch s {
	case ReadingReviewNotRequired, ReadingReviewUnverified, ReadingReviewPendingReview, ReadingReviewApproved, ReadingReviewRejected:
		return true
	}
	return false
}

type ExecutionStatus string

const (
	ExecutionScheduled ExecutionStatus = "scheduled"
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionCompleted ExecutionStatus = "completed"
	ExecutionCancelled ExecutionStatus = "cancelled"
)
