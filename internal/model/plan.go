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
	// GSTMode controls how GST is applied to PriceRupees:
	//   "none"      — no GST (PriceRupees is the final amount, no tax shown).
	//   "inclusive" — PriceRupees already contains GST; the invoice splits it
	//                 into base + GST. Amount charged = PriceRupees.
	//   "exclusive" — GST is added on top; amount charged = PriceRupees + GST.
	GSTMode string `gorm:"size:12;not null;default:'none'" json:"gst_mode"`
	// GSTRate is the GST percentage applied when GSTMode != "none" (e.g. 18).
	GSTRate int `gorm:"not null;default:18" json:"gst_rate"`
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

// GSTApplicable reports whether this plan carries GST (inclusive or exclusive).
func (p *Plan) GSTApplicable() bool {
	return p.GSTMode == "inclusive" || p.GSTMode == "exclusive"
}

// PayablePaise is the amount actually charged to the customer, in paise. For an
// exclusive-GST plan this is price + GST; otherwise it is the plain price (an
// inclusive plan already has the tax baked into PriceRupees).
func (p *Plan) PayablePaise() int64 {
	base := int64(p.PriceRupees) * 100
	if p.GSTMode == "exclusive" {
		return base + base*int64(p.GSTRate)/100
	}
	return base
}

// InvoiceSplitPaise breaks a charged total (in paise) into its taxable base and
// GST components for the invoice. For a non-GST plan the whole amount is the
// base and GST is zero. Both inclusive and exclusive split the final total the
// same way: base = total / (1 + rate), GST = total − base.
func (p *Plan) InvoiceSplitPaise(totalPaise int64) (basePaise, gstPaise int64) {
	if !p.GSTApplicable() || p.GSTRate <= 0 {
		return totalPaise, 0
	}
	basePaise = totalPaise * 100 / (100 + int64(p.GSTRate))
	return basePaise, totalPaise - basePaise
}
