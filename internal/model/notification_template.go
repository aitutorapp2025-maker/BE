package model

import "time"

// NotificationTemplate is the admin-editable wording for one automated
// notification type. Dynamic values are filled from {placeholders} at send time;
// a blank title/body falls back to the built-in default, and Active=false turns
// that automated notification off entirely.
type NotificationTemplate struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Type      string    `gorm:"uniqueIndex;size:40;not null" json:"type"`
	Title     string    `gorm:"size:200" json:"title"`
	Body      string    `gorm:"size:1000" json:"body"`
	Active    bool      `gorm:"not null;default:true" json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (NotificationTemplate) TableName() string { return "notification_templates" }

// NotifTemplateDef describes a template's default wording and the placeholders
// it supports (shown in the admin UI).
type NotifTemplateDef struct {
	Type         string
	Title        string
	Body         string
	Placeholders []string
	Label        string // human label for the admin UI
}

// DefaultNotificationTemplates is the single source of truth for both seeding
// the table and the in-code fallback when a template row is missing/blank.
func DefaultNotificationTemplates() []NotifTemplateDef {
	return []NotifTemplateDef{
		{Type: "credit_added", Label: "Credits added (top-up / recharge)",
			Title: "Credits added ⚡",
			Body:  "{credits} credits added — your balance is now {balance}.",
			Placeholders: []string{"credits", "balance"}},
		{Type: "credit_low", Label: "Out of credits",
			Title: "You're out of credits ⚡",
			Body:  "Top up to keep using the AI tutor — open the app and buy credits.",
			Placeholders: []string{}},
		{Type: "payment_success", Label: "Payment successful",
			Title: "Payment successful ✅",
			Body:  "Your {plan} plan is active — {credits} credits added for this cycle.",
			Placeholders: []string{"plan", "credits"}},
		{Type: "payment_failed", Label: "Payment failed",
			Title: "Payment failed ⚠️",
			Body:  "We couldn't renew your plan. Please check your UPI AutoPay or update your payment method to keep your access.",
			Placeholders: []string{}},
		{Type: "renewal_reminder", Label: "Plan renewal reminder",
			Title: "Your plan renews soon 🔄",
			Body:  "Your Vaha AI plan renews on {date}. Keep your UPI AutoPay active to avoid interruption.",
			Placeholders: []string{"date"}},
		{Type: "referral_promo", Label: "Referral promo",
			Title: "Invite friends, earn rewards 🎁",
			Body:  "Get ₹{reward} off your next bill for every friend who joins Vaha AI with your code. Open the app to share your link!",
			Placeholders: []string{"reward"}},
	}
}

// DefaultNotifTemplate returns the default title/body for a type ("","" if none).
func DefaultNotifTemplate(typ string) (title, body string) {
	for _, d := range DefaultNotificationTemplates() {
		if d.Type == typ {
			return d.Title, d.Body
		}
	}
	return "", ""
}
