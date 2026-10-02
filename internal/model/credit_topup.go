package model

import "time"

// CreditTopup tracks a one-time credit purchase from order creation to payment,
// so the credit grant on verify is idempotent and never trusts a client-sent
// credit amount. Status: "created" → "paid".
type CreditTopup struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	StudentID   uint      `gorm:"index;not null" json:"student_id"`
	OrderID     string    `gorm:"uniqueIndex;size:64;not null" json:"order_id"`
	PaymentID   string    `gorm:"size:64" json:"payment_id"`
	AmountPaise int       `gorm:"not null" json:"amount_paise"`
	Credits     int       `gorm:"not null" json:"credits"`
	Status      string    `gorm:"size:12;not null;default:'created'" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (CreditTopup) TableName() string { return "credit_topups" }
