package model

import (
	"aquaculture-water-feeding-control/backend/internal/constants"
	"time"
)

type WaterReading struct {
	Base
	PondID          uint                    `gorm:"not null;index" json:"pondId"`
	Pond            *Pond                   `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"pond,omitempty"`
	DissolvedOxygen float64                 `gorm:"not null" json:"dissolvedOxygen"`
	Temperature     float64                 `gorm:"not null" json:"temperature"`
	PH              float64                 `gorm:"column:ph;not null" json:"ph"`
	Ammonia         float64                 `gorm:"not null" json:"ammonia"`
	Turbidity       float64                 `gorm:"not null" json:"turbidity"`
	MeasuredAt      time.Time               `gorm:"not null;index" json:"measuredAt"`
	Source          string                  `gorm:"size:30;not null" json:"source"`
	RiskLevel       constants.RiskLevel     `gorm:"size:20;not null;index" json:"riskLevel"`
	AlertMessage    string                  `gorm:"type:text" json:"alertMessage"`
	ReviewStatus    constants.ReadingReview `gorm:"size:20;not null;default:not_required;index" json:"reviewStatus"`
	// 首名操作员现场核实后生成“待复核”记录；第二名操作员通过或否决后关闭闭环。
	VerifiedByUserID uint       `gorm:"index" json:"verifiedByUserId"`
	VerifiedBy       string     `gorm:"size:80" json:"verifiedBy"`
	VerifiedAt       *time.Time `json:"verifiedAt"`
	ReviewByUserID   uint       `gorm:"index" json:"reviewByUserId"`
	ReviewBy         string     `gorm:"size:80" json:"reviewBy"`
	ReviewAt         *time.Time `json:"reviewAt"`
	ReviewNote       string     `gorm:"type:text" json:"reviewNote"`
	// Confirmed 在闭环完成（预警单级确认或严重双人通过）后才为 true，
	// 供计划批准、投喂安排等既有放行逻辑直接使用。
	Confirmed        bool       `gorm:"not null;default:false" json:"confirmed"`
	ConfirmedBy      string     `gorm:"size:80" json:"confirmedBy"`
	ConfirmedAt      *time.Time `json:"confirmedAt"`
	ConfirmationNote string     `gorm:"type:text" json:"confirmationNote"`
}
