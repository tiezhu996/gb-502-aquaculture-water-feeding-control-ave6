package model

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"time"
)

type WaterReading struct {
	Base
	PondID           uint                `gorm:"not null;index" json:"pondId"`
	Pond             *Pond               `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"pond,omitempty"`
	DissolvedOxygen  float64             `gorm:"not null" json:"dissolvedOxygen"`
	Temperature      float64             `gorm:"not null" json:"temperature"`
	PH               float64             `gorm:"column:ph;not null" json:"ph"`
	Ammonia          float64             `gorm:"not null" json:"ammonia"`
	Turbidity        float64             `gorm:"not null" json:"turbidity"`
	MeasuredAt       time.Time           `gorm:"not null;index" json:"measuredAt"`
	Source           string              `gorm:"size:30;not null" json:"source"`
	RiskLevel        constants.RiskLevel `gorm:"size:20;not null;index" json:"riskLevel"`
	AlertMessage     string              `gorm:"type:text" json:"alertMessage"`
	Confirmed        bool                `gorm:"not null;default:false" json:"confirmed"`
	ConfirmedBy      string              `gorm:"size:80" json:"confirmedBy"`
	ConfirmedAt      *time.Time          `json:"confirmedAt"`
	ConfirmationNote string              `gorm:"type:text" json:"confirmationNote"`

	// 严重读数双人复核闭环。ReviewStatus 为空表示无需双人复核（正常/预警读数）。
	// 一级核实（Verify*）只生成待复核记录；二级复核（Review*）由另一名操作员
	// 通过或否决。最终通过时 Confirmed* 字段记录二级复核人，作为读数生效凭证。
	ReviewStatus     constants.ReadingReviewStatus `gorm:"size:20;not null;default:'';index" json:"reviewStatus"`
	VerifiedByUserID uint                          `gorm:"not null;default:0" json:"verifiedByUserId"`
	VerifiedBy       string                        `gorm:"size:80" json:"verifiedBy"`
	VerifiedAt       *time.Time                    `json:"verifiedAt"`
	VerificationNote string                        `gorm:"type:text" json:"verificationNote"`
	ReviewedByUserID uint                          `gorm:"not null;default:0" json:"reviewedByUserId"`
	ReviewedBy       string                        `gorm:"size:80" json:"reviewedBy"`
	ReviewedAt       *time.Time                    `json:"reviewedAt"`
	ReviewNote       string                        `gorm:"type:text" json:"reviewNote"`
	RejectionReason  string                        `gorm:"type:text" json:"rejectionReason"`
}
