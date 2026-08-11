package model

import "time"

// CreditLedger records every change to a student's AI credit balance — plan
// grants and recharges (money in), and debits (AI usage). It is the source of
// truth for the admin profit & loss view: revenue = Σ RevenuePaise, AI cost =
// Σ AICostPaise, profit = revenue − cost.
type CreditLedger struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	// Covered by the composite idx_credit_student_created — no standalone index.
	StudentID uint   `gorm:"not null" json:"student_id"`
	Kind      string `gorm:"size:20;not null" json:"kind"` // grant | recharge | debit
	Action    string `gorm:"size:40" json:"action"`        // debit only: ask_text, oral_exam, …
	Credits   int    `gorm:"not null" json:"credits"`      // signed: +grant/recharge, −debit
	// RevenuePaise is money received (plan grant / recharge), in paise. 0 for debits.
	RevenuePaise int64 `gorm:"not null;default:0" json:"revenue_paise"`
	// AICostPaise is the estimated AI cost of a debit, in paise. 0 for grants.
	AICostPaise int64  `gorm:"not null;default:0" json:"ai_cost_paise"`
	Note        string `gorm:"size:200" json:"note"`

	CreatedAt time.Time `json:"created_at"`
}

// TableName sets the table name explicitly.
func (CreditLedger) TableName() string { return "credit_ledger" }
