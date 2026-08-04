package model

import "time"

// Referral is one attribution record: a new student (Referee) signed up under a
// referrer's code, earning the referrer RewardRupees off their next bill. The
// RefereeID unique index guarantees a student can only ever be attributed once,
// so a referrer can't farm rewards by re-linking the same account.
type Referral struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	ReferrerID   uint   `gorm:"index;not null" json:"referrer_id"`
	RefereeID    uint   `gorm:"uniqueIndex;not null" json:"referee_id"`
	Code         string `gorm:"size:16;index" json:"code"`
	RewardRupees int    `json:"reward_rupees"`
	// Denormalised names for the admin list (avoids a join on every view).
	ReferrerName string    `gorm:"size:120" json:"referrer_name"`
	RefereeName  string    `gorm:"size:120" json:"referee_name"`
	CreatedAt    time.Time `json:"created_at"`
}

// TableName sets the table name explicitly.
func (Referral) TableName() string { return "referrals" }
