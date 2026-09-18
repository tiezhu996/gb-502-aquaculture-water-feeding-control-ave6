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

// ReadingReviewStatus 仅对严重（critical）读数使用：首名操作员核实后进入待复核，
// 必须由另一名操作员通过或否决后才闭环。正常/预警读数保持单级确认，取值留空。
type ReadingReviewStatus string

const (
	ReviewPending  ReadingReviewStatus = "pending"  // 一级核实完成，等待另一人二级复核
	ReviewApproved ReadingReviewStatus = "approved" // 二级复核通过，读数生效
	ReviewRejected ReadingReviewStatus = "rejected" // 二级复核否决，保持严重并写明原因
)

func (s ReadingReviewStatus) Valid() bool {
	return s == "" || s == ReviewPending || s == ReviewApproved || s == ReviewRejected
}

type ExecutionStatus string

const (
	ExecutionScheduled ExecutionStatus = "scheduled"
	ExecutionRunning   ExecutionStatus = "running"
	ExecutionCompleted ExecutionStatus = "completed"
	ExecutionCancelled ExecutionStatus = "cancelled"
)
