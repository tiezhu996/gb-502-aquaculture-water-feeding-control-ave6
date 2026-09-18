package dto

import "time"

type WaterReadingInput struct {
	PondID          uint      `json:"pondId" binding:"required"`
	DissolvedOxygen float64   `json:"dissolvedOxygen" binding:"gte=0,lte=30"`
	Temperature     float64   `json:"temperature" binding:"gte=-5,lte=50"`
	PH              float64   `json:"ph" binding:"gte=0,lte=14"`
	Ammonia         float64   `json:"ammonia" binding:"gte=0,lte=20"`
	Turbidity       float64   `json:"turbidity" binding:"gte=0,lte=1000"`
	MeasuredAt      time.Time `json:"measuredAt" binding:"required"`
	Source          string    `json:"source" binding:"required,oneof=sensor manual import"`
}

// ConfirmReadingInput 用于预警读数的单级确认，以及严重读数首名操作员的一级核实。
type ConfirmReadingInput struct {
	Note string `json:"note" binding:"required,min=2,max=500"`
}

// RejectReadingInput 用于第二名操作员否决严重读数，必须写明否决原因。
type RejectReadingInput struct {
	Reason string `json:"reason" binding:"required,min=10,max=500"`
}
