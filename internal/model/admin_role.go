package model

import (
	"strings"
	"time"
)

// AdminRole is an admin-panel role: a named set of permission keys (see the
// catalog below). An admin whose RoleID is nil is a SUPER admin with full,
// unrestricted access — the seeded first admin works this way, so the panel is
// never locked out even before any roles exist.
type AdminRole struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"size:80;uniqueIndex;not null" json:"name"`
	Description string    `gorm:"size:255" json:"description"`
	Permissions []string  `gorm:"serializer:json" json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// AdminCount is filled by the list query (how many admins use this role).
	AdminCount int64 `gorm:"-" json:"admin_count"`
}

// TableName sets the table name explicitly.
func (AdminRole) TableName() string { return "admin_roles" }

// ─── Permission catalog ─────────────────────────────────────────────────────
//
// One key per admin side-menu item plus one per Settings tab. The FE mirrors
// this list (admin_permissions.dart) to filter the sidebar and the tabs; the
// backend enforces it per-route in middleware.AdminPermission.

// PermDef describes one grantable permission for the role editor UI.
type PermDef struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Group string `json:"group"` // "Side menu" | "Settings tabs"
}

// Side-menu permission keys.
const (
	PermDashboard   = "dashboard"
	PermStudents    = "students"
	PermPlans       = "plans"
	PermClasses     = "classes"
	PermBooks       = "books"
	PermTeachLangs  = "teaching_languages"
	PermLanding     = "landing"
	PermEnquiries   = "enquiries"
	PermLegal       = "legal"
	PermReports     = "reports"
	PermAnalytics   = "analytics"
	PermCrashlytics = "crashlytics"
	PermSupport     = "support"
	PermCrons       = "crons"
	PermNotify      = "notifications"
	PermAuditLogs   = "audit_logs"
	PermAdminUsers  = "admin_users"
	PermBanners     = "banners"
)

// Settings-tab permission keys. (The Account tab — the admin's own password —
// is always available and is intentionally NOT a grantable permission.)
const (
	PermSetOrganisation = "settings.organisation"
	PermSetPreferences  = "settings.preferences"
	PermSetReferral     = "settings.referral"
	PermSetEmail        = "settings.email"
	PermSetSMS          = "settings.sms"
	PermSetAI           = "settings.ai"
	PermSetPayment      = "settings.payment"
	PermSetCaptcha      = "settings.captcha"
	PermSetAlerts       = "settings.alerts"
	PermSetMaintenance  = "settings.maintenance"
	PermSetAppUpdate    = "settings.app_update"
	PermSetPush         = "settings.push"
	PermSetSSO          = "settings.sso"
	PermSetStatus       = "settings.status"
	PermSetAnalytics    = "settings.analytics"
	PermSetCrashlytics  = "settings.crashlytics"
)

// AllPermissions is the grantable catalog, in display order, served to the
// role editor (GET /admin/permissions).
var AllPermissions = []PermDef{
	{PermDashboard, "Dashboard", "Side menu"},
	{PermStudents, "Students", "Side menu"},
	{PermPlans, "Plans", "Side menu"},
	{PermClasses, "Class master (incl. groups)", "Side menu"},
	{PermBooks, "Books", "Side menu"},
	{PermTeachLangs, "Teaching Languages", "Side menu"},
	{PermLanding, "Landing Page CMS", "Side menu"},
	{PermEnquiries, "Enquiries", "Side menu"},
	{PermLegal, "Terms & Conditions", "Side menu"},
	{PermReports, "Reports", "Side menu"},
	{PermAnalytics, "Analytics", "Side menu"},
	{PermCrashlytics, "Crashlytics", "Side menu"},
	{PermSupport, "Support tickets", "Side menu"},
	{PermCrons, "Cron jobs", "Side menu"},
	{PermNotify, "Send notification", "Side menu"},
	{PermAuditLogs, "Audit logs", "Side menu"},
	{PermAdminUsers, "Admin users & roles", "Side menu"},
	{PermBanners, "Home banners", "Side menu"},

	{PermSetOrganisation, "Organisation", "Settings tabs"},
	{PermSetPreferences, "Preferences", "Settings tabs"},
	{PermSetReferral, "Referral", "Settings tabs"},
	{PermSetEmail, "Email (SMTP)", "Settings tabs"},
	{PermSetSMS, "SMS", "Settings tabs"},
	{PermSetAI, "AI Tutor", "Settings tabs"},
	{PermSetPayment, "Payment Gateway", "Settings tabs"},
	{PermSetCaptcha, "Captcha", "Settings tabs"},
	{PermSetAlerts, "Error alerts", "Settings tabs"},
	{PermSetMaintenance, "Maintenance", "Settings tabs"},
	{PermSetAppUpdate, "App update", "Settings tabs"},
	{PermSetPush, "Push (FCM)", "Settings tabs"},
	{PermSetSSO, "Login / SSO", "Settings tabs"},
	{PermSetStatus, "System status", "Settings tabs"},
	{PermSetAnalytics, "Analytics tab", "Settings tabs"},
	{PermSetCrashlytics, "Crashlytics tab", "Settings tabs"},
}

// ValidPermission reports whether key is in the grantable catalog.
func ValidPermission(key string) bool {
	for _, p := range AllPermissions {
		if p.Key == key {
			return true
		}
	}
	return false
}

// settingsPerms is every settings.* key — "any of these" grants saving Settings.
func settingsPerms() []string {
	out := make([]string, 0, 16)
	for _, p := range AllPermissions {
		if strings.HasPrefix(p.Key, "settings.") {
			out = append(out, p.Key)
		}
	}
	return out
}

// RequiredPermsForAdminRoute maps an admin API request to the permission keys
// that allow it (ANY one suffices). An empty result means every signed-in
// admin may call it (own account/session endpoints, reading the settings
// singleton the page needs to render).
//
// [seg] is the path after "/api/v1/admin/" (e.g. "students/5/ledger").
func RequiredPermsForAdminRoute(method, seg string) []string {
	seg = strings.TrimPrefix(seg, "/")
	head, rest, _ := strings.Cut(seg, "/")

	switch head {
	case "me", "logout", "change-password":
		return nil // own-session endpoints — always allowed
	case "dashboard", "billing":
		return []string{PermDashboard}
	case "status":
		return []string{PermSetStatus}
	case "students":
		return []string{PermStudents}
	case "plans":
		return []string{PermPlans}
	case "classes", "class-groups":
		return []string{PermClasses}
	case "books":
		return []string{PermBooks}
	case "teaching-languages":
		return []string{PermTeachLangs}
	case "landing":
		return []string{PermLanding}
	case "contacts":
		return []string{PermEnquiries}
	case "legal":
		return []string{PermLegal}
	case "reports":
		return []string{PermReports}
	case "analytics":
		return []string{PermAnalytics}
	case "crashlytics":
		return []string{PermCrashlytics}
	case "firebase-stats":
		return []string{PermAnalytics, PermCrashlytics}
	case "support":
		return []string{PermSupport}
	case "crons":
		return []string{PermCrons}
	case "notifications":
		return []string{PermNotify}
	case "audit-logs":
		return []string{PermAuditLogs}
	case "referrals":
		return []string{PermSetReferral}
	case "admins", "roles", "permissions":
		return []string{PermAdminUsers}
	case "banners":
		return []string{PermBanners}
	case "settings":
		sub, _, _ := strings.Cut(rest, "/")
		switch sub {
		case "logo":
			return []string{PermSetOrganisation}
		case "test-email":
			return []string{PermSetEmail}
		case "test-sms":
			return []string{PermSetSMS}
		case "test-ai":
			return []string{PermSetAI}
		}
		// The singleton itself: reading it is needed just to render the page
		// (secrets are already masked); saving needs any settings tab.
		if method == "GET" {
			return nil
		}
		return settingsPerms()
	}
	// Unknown/new route — locked to super admins until it's mapped here.
	return []string{"__unmapped__"}
}
