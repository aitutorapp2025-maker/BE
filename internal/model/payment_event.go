package model

import "time"

// PaymentEvent records a processed Razorpay charge so the webhook is idempotent
// — Razorpay may deliver the same event more than once, and we must never grant
// credits twice for one payment. The Razorpay payment id (pay_...) is unique.
type PaymentEvent struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	PaymentID   string    `gorm:"size:60;uniqueIndex;not null" json:"payment_id"`
	Event       string    `gorm:"size:60" json:"event"`
	StudentID   uint      `gorm:"index" json:"student_id"`
	AmountPaise int64     `json:"amount_paise"`
	CreatedAt   time.Time `json:"created_at"`
}

// TableName sets the table name explicitly.
func (PaymentEvent) TableName() string { return "payment_events" }
