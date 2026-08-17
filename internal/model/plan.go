package model

import "time"

// Plan is a subscription plan managed from the admin panel.
type Plan struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Name         string `gorm:"size:80;not null" json:"name"`
	PriceRupees  int    `gorm:"not null;default:0" json:"price_rupees"`
	MrpRupees    *int   `json:"mrp_rupees"` // original price when discounted (nullable)
	DurationDays int    `gorm:"not null;default:30" json:"duration_days"`
	// Credits granted per billing cycle. 1 credit = ₹1 of AI-cost budget, so
	// sizing credits at 15% of PriceRupees guarantees ≥85% gross margin even if
	// a student spends every credit (see internal/service/credit_service.go).
	Credits int `gorm:"not null;default:0" json:"credits"`
	// IsTrial marks the free-trial plan that new students get on onboarding.
	// Admin controls the trial length (DurationDays) and credits (Credits) by
	// editing this plan. There should be exactly one trial plan.
	IsTrial bool `gorm:"not null;default:false" json:"is_trial"`
	// RazorpayPlanID links this tier to a LIVE-mode Razorpay plan (plan_...)
	// for UPI AutoPay subscriptions; RazorpayTestPlanID is its test-mode twin
	// (auto-created — Razorpay plan ids are mode-scoped, so each mode needs its
	// own). Blank = not (yet) linked in that mode.
	RazorpayPlanID     string `gorm:"size:60" json:"razorpay_plan_id"`
	RazorpayTestPlanID string `gorm:"size:60" json:"razorpay_test_plan_id"`
	Tagline        string    `gorm:"size:120" json:"tagline"`
	Features       []string  `gorm:"serializer:json" json:"features"`
	BestValue      bool      `gorm:"not null;default:false" json:"best_value"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (Plan) TableName() string { return "plans" }

// RzpPlanID returns the Razorpay plan id for the given mode.
func (p *Plan) RzpPlanID(test bool) string {
	if test {
		return p.RazorpayTestPlanID
	}
	return p.RazorpayPlanID
}

// SetRzpPlanID stores the Razorpay plan id for the given mode.
func (p *Plan) SetRzpPlanID(test bool, id string) {
	if test {
		p.RazorpayTestPlanID = id
	} else {
		p.RazorpayPlanID = id
	}
}
