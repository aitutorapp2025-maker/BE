package model

import "time"

// CreditPack is an admin-managed one-time credit top-up option shown to students
// when their balance hits 0 (e.g. ₹25 → 4 credits). Students can also top up a
// custom amount, priced at their plan rate (credits ÷ price).
type CreditPack struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	AmountRupees int       `gorm:"not null" json:"amount_rupees"`
	Credits      int       `gorm:"not null" json:"credits"`
	Active       bool      `gorm:"not null;default:true" json:"active"`
	Sort         int       `gorm:"not null;default:0" json:"sort"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (CreditPack) TableName() string { return "credit_packs" }
