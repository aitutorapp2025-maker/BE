package model

import (
	"time"

	"gorm.io/gorm"
)

// Student is a learner account managed from the admin panel.
type Student struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Name         string `gorm:"size:120;not null" json:"name"`
	Phone        string `gorm:"size:20" json:"phone"`
	ParentPhone  string `gorm:"size:20" json:"parent_phone"`
	// Email + GoogleID are set for Google (SSO) sign-in. A Google-first account
	// may have no phone until the student adds one.
	Email    string `gorm:"size:190;index" json:"email"`
	GoogleID string `gorm:"size:60;index" json:"google_id"`
	StudentClass string `gorm:"size:40;not null" json:"student_class"` // e.g. "Class 10"
	Board        string `gorm:"size:40;not null" json:"board"`         // State Board / CBSE / ICSE
	Medium       string `gorm:"size:20;not null" json:"medium"`        // English / Tamil
	// StudentGroup is the subject group / stream, asked only for classes that
	// have groups configured (higher secondary — 11 & 12). Blank otherwise.
	StudentGroup string `gorm:"size:60" json:"student_group"` // e.g. "Computer Science"
	// TeachingLanguage is the admin-managed language of instruction (Tamil /
	// English / Hindi / Telugu ...).
	TeachingLanguage string `gorm:"size:40" json:"teaching_language"`
	Plan             string `gorm:"size:20;not null;default:trial" json:"plan"`       // trial | monthly | yearly
	PayStatus        string `gorm:"size:20;not null;default:trial" json:"pay_status"` // trial | paid | expired
	// Credits is the student's current AI balance. Reduced as they use the AI
	// (see credit_service). Topped up by a plan grant or a recharge.
	Credits int `gorm:"not null;default:0" json:"credits"`
	// RazorpaySubscriptionID is the active UPI AutoPay subscription (sub_...),
	// set when the student subscribes; the webhook matches charges back to them.
	RazorpaySubscriptionID string `gorm:"size:60;index" json:"razorpay_subscription_id"`
	// Headless UPI-AutoPay (Orders+Tokens intent flow). RazorpayCustomerID is the
	// Razorpay customer (cust_…) reused across mandates; RazorpayTokenID is the
	// authorized mandate token (token_…) used to trigger each recurring debit
	// ourselves; SubscribedPlanID is the plan we auto-debit; NextChargeAt is when
	// the scheduler should trigger the next debit. These are empty for students on
	// the legacy Subscriptions checkout.
	RazorpayCustomerID string     `gorm:"size:60;index" json:"razorpay_customer_id"`
	RazorpayTokenID    string     `gorm:"size:60;index" json:"razorpay_token_id"`
	SubscribedPlanID   uint       `gorm:"index" json:"subscribed_plan_id"`
	NextChargeAt       *time.Time `gorm:"index" json:"next_charge_at"`
	// TrialEndsAt is when the free trial expires. The base-plan subscription is
	// scheduled to auto-debit at this time (Razorpay start_at).
	TrialEndsAt *time.Time `json:"trial_ends_at"`
	// AutopayActive is true once the student has an authorized UPI-AutoPay
	// mandate (set by the Razorpay webhook). The trial requires it — if the
	// student never enables it (or deletes it), the trial can't be used and
	// expires with no auto-debit.
	AutopayActive bool      `gorm:"not null;default:false" json:"autopay_active"`

	// Referral program. ReferralCode is this student's own share code, generated
	// lazily the first time they open Refer & Earn — so most rows have '' until
	// then. Uniqueness is enforced by a PARTIAL unique index (only non-empty,
	// non-deleted codes) created in CreateIndexes, NOT a plain uniqueIndex tag: a
	// plain unique index collides on the shared '' across students (SQLSTATE
	// 23505). ReferredByID is the referrer's id, set once at signup under a code.
	// ReferralRewardRupees is an accrued discount applied to — and cleared at — the
	// next auto-debit. ReferralCount is how many students have joined via this code.
	ReferralCode         string `gorm:"size:16" json:"referral_code"`
	ReferredByID         uint   `gorm:"index" json:"referred_by_id"`
	ReferralRewardRupees int    `gorm:"not null;default:0" json:"referral_reward_rupees"`
	ReferralCount        int    `gorm:"not null;default:0" json:"referral_count"`

	JoinedAt time.Time `json:"joined_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	// DeletedAt enables soft delete: a deleted account keeps its row (audit /
	// history) but is excluded from every query, so re-registering with the same
	// phone creates a fresh record instead of resurfacing the old one.
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName sets the table name explicitly.
func (Student) TableName() string { return "students" }
