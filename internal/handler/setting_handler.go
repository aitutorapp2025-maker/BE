package handler

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/fcm"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/sms"
	"github.com/gofiber/fiber/v2"
)

// SettingHandler exposes the app settings endpoints (admin only).
type SettingHandler struct {
	settings *repository.SettingRepository
	mailer   *email.Publisher
	smser    *sms.Publisher
	// aiProbe verifies the configured AI keys (Voyage + Claude). nil when the
	// tutoring pipeline isn't wired (e.g. pgvector missing).
	aiProbe func(context.Context) error
}

// NewSettingHandler builds a SettingHandler. aiProbe may be nil.
func NewSettingHandler(settings *repository.SettingRepository, mailer *email.Publisher, smser *sms.Publisher, aiProbe func(context.Context) error) *SettingHandler {
	return &SettingHandler{settings: settings, mailer: mailer, smser: smser, aiProbe: aiProbe}
}

type settingRequest struct {
	AppName            string `json:"app_name"`
	SupportEmail       string `json:"support_email"`
	SupportWhatsApp    string `json:"support_whatsapp"`
	LogoURL            string `json:"logo_url"`
	EmailNotifications bool   `json:"email_notifications"`
	AutoApproveAnswers bool   `json:"auto_approve_answers"`
	MaintenanceMode    bool   `json:"maintenance_mode"`

	MaintenanceWeb     bool   `json:"maintenance_web"`
	MaintenanceMobile  bool   `json:"maintenance_mobile"`
	MaintenanceTitle   string `json:"maintenance_title"`
	MaintenanceMessage string `json:"maintenance_message"`

	LatestAppVersion string `json:"latest_app_version"`
	MinAppVersion    string `json:"min_app_version"`
	ForceUpdate      bool   `json:"force_update"`
	UpdateMessage    string `json:"update_message"`
	AndroidStoreURL  string `json:"android_store_url"`
	IosStoreURL      string `json:"ios_store_url"`

	// Uploaded Firebase service-account JSON (write-only; empty = keep existing).
	FcmServiceAccount string `json:"fcm_service_account"`

	GoogleSsoEnabled bool   `json:"google_sso_enabled"`
	GoogleClientID   string `json:"google_client_id"`

	// SMTP. Password is write-only: empty means "keep the existing password".
	SmtpEnabled  bool   `json:"smtp_enabled"`
	SmtpHost     string `json:"smtp_host"`
	SmtpPort     string `json:"smtp_port"`
	SmtpUser     string `json:"smtp_user"`
	SmtpPassword string `json:"smtp_password"`
	SmtpFrom     string `json:"smtp_from"`
	SmtpFromName string `json:"smtp_from_name"`

	ErrorAlertsEnabled bool   `json:"error_alerts_enabled"`
	AlertEmail         string `json:"alert_email"`

	// SMS. Secret fields are write-only: empty means "keep the existing one".
	SmsEnabled        bool   `json:"sms_enabled"`
	SmsProvider       string `json:"sms_provider"`
	SmsCountryCode    string `json:"sms_country_code"`
	NexmoAPIKey       string `json:"nexmo_api_key"`
	NexmoAPISecret    string `json:"nexmo_api_secret"`
	NexmoFrom         string `json:"nexmo_from"`
	SmsExpertAPIURL   string `json:"smsexpert_api_url"`
	SmsExpertUser     string `json:"smsexpert_user"`
	SmsExpertPassword string `json:"smsexpert_password"`
	SmsExpertSender   string `json:"smsexpert_sender"`
	SmsExpertRoute    string `json:"smsexpert_route"`
	SmsExpertType     string `json:"smsexpert_type"`

	// Test OTP (admin-listed numbers get the fixed code, no SMS).
	TestOtpEnabled bool   `json:"test_otp_enabled"`
	TestOtpCode    string `json:"test_otp_code"`
	TestOtpPhones  string `json:"test_otp_phones"`

	// WhatsApp (parents' daily report). Token is write-only: empty keeps it.
	WhatsappEnabled      bool   `json:"whatsapp_enabled"`
	WhatsappToken        string `json:"whatsapp_token"`
	WhatsappPhoneID      string `json:"whatsapp_phone_id"`
	WhatsappTemplate     string `json:"whatsapp_template"`
	WhatsappTemplateLang string `json:"whatsapp_template_lang"`

	// CAPTCHA. Secret is write-only.
	CaptchaEnabled  bool   `json:"captcha_enabled"`
	CaptchaProvider string `json:"captcha_provider"`
	CaptchaSiteKey  string `json:"captcha_site_key"`
	CaptchaSecret   string `json:"captcha_secret"`

	// AI tutor. Keys are write-only: empty means "keep the existing one".
	AIEnabled       bool   `json:"ai_enabled"`
	AnthropicAPIKey string `json:"anthropic_api_key"`
	AnthropicModel  string `json:"anthropic_model"`
	AnswersProvider string `json:"answers_provider"` // claude | gemini
	GeminiAPIKey    string `json:"gemini_api_key"`
	GeminiModel     string `json:"gemini_model"`
	VoyageAPIKey    string `json:"voyage_api_key"`
	VoyageModel     string `json:"voyage_model"`
	// Embeddings backend selection (voyage | local) + local endpoint.
	EmbeddingsProvider string `json:"embeddings_provider"`
	LocalEmbedURL      string `json:"local_embed_url"`
	LocalEmbedModel    string `json:"local_embed_model"`

	// Razorpay. Secrets are write-only; key ids are not secrets. Both the live
	// and test key sets are stored; RazorpayTestMode picks the active one.
	RazorpayEnabled           bool   `json:"razorpay_enabled"`
	RazorpayKeyID             string `json:"razorpay_key_id"`
	RazorpayKeySecret         string `json:"razorpay_key_secret"`
	RazorpayWebhookSecret     string `json:"razorpay_webhook_secret"`
	RazorpayTestMode          bool   `json:"razorpay_test_mode"`
	RazorpayTestKeyID         string `json:"razorpay_test_key_id"`
	RazorpayTestKeySecret     string `json:"razorpay_test_key_secret"`
	RazorpayTestWebhookSecret string `json:"razorpay_test_webhook_secret"`

	// Whether trials require a UPI-AutoPay mandate (admin-toggleable).
	AutopayEnabled bool `json:"autopay_enabled"`
	// Whether the "Cast to TV" option shows on the app home page.
	CastEnabled bool `json:"cast_enabled"`
	// Whether per-task countdown timers + alerts are on.
	TimedTasksEnabled bool `json:"timed_tasks_enabled"`

	// Referral ("refer & earn") program.
	ReferralEnabled      bool   `json:"referral_enabled"`
	ReferralRewardRupees int    `json:"referral_reward_rupees"`
	ReferralShareMessage string `json:"referral_share_message"`
	ReferralLinkBase     string `json:"referral_link_base"`

	// Firebase Analytics + Crashlytics dashboards (BigQuery export).
	AnalyticsEnabled   bool   `json:"analytics_enabled"`
	CrashlyticsEnabled bool   `json:"crashlytics_enabled"`
	BigQueryProjectID  string `json:"bigquery_project_id"`
	AnalyticsDataset   string `json:"analytics_dataset"`
	CrashlyticsDataset string `json:"crashlytics_dataset"`
	CrashlyticsTable   string `json:"crashlytics_table"`
	CrashTestEnabled   bool   `json:"crash_test_enabled"`
	ChatSoundsEnabled  bool   `json:"chat_sounds_enabled"`
	ProfilePasswordEnabled bool `json:"profile_password_enabled"`
	BiometricEnabled       bool `json:"biometric_enabled"`
}

// Maintenance returns the public maintenance status for the customer apps.
// GET /api/v1/maintenance (no auth). Each app checks its own platform flag; the
// admin panel never calls this, so admins are unaffected.
func (h *SettingHandler) Maintenance(c *fiber.Ctx) error {
	s, err := h.settings.Get()
	if err != nil {
		// Fail open — never lock customers out because settings couldn't load.
		return c.JSON(fiber.Map{"success": true, "web": false, "mobile": false})
	}
	return c.JSON(fiber.Map{
		"success": true,
		"web":     s.MaintenanceWeb,
		"mobile":  s.MaintenanceMobile,
		"title":   s.MaintenanceTitle,
		"message": s.MaintenanceMessage,
	})
}

// AppVersion tells the mobile app whether an update is available and whether it
// is mandatory. GET /api/v1/app-version?platform=android&version=1.0.0 (no auth).
func (h *SettingHandler) AppVersion(c *fiber.Ctx) error {
	s, err := h.settings.Get()
	if err != nil {
		return c.JSON(fiber.Map{"success": true, "update_available": false})
	}
	platform := strings.ToLower(strings.TrimSpace(c.Query("platform")))
	current := strings.TrimSpace(c.Query("version"))
	latest := strings.TrimSpace(s.LatestAppVersion)
	minv := strings.TrimSpace(s.MinAppVersion)

	// An update is "available" when a newer version is published, OR when the
	// admin flips the blanket "Force update" switch (a manual override to make
	// everyone update now, regardless of the version numbers).
	updateAvailable := (latest != "" && cmpVersion(current, latest) < 0) ||
		s.ForceUpdate
	// Mandatory when the app is below the minimum supported version, or the
	// blanket Force-update switch is on.
	force := (minv != "" && cmpVersion(current, minv) < 0) || s.ForceUpdate

	storeURL := s.AndroidStoreURL
	if platform == "ios" {
		storeURL = s.IosStoreURL
	}
	return c.JSON(fiber.Map{
		"success":          true,
		"update_available": updateAvailable,
		"force":            force,
		"latest":           latest,
		"message":          s.UpdateMessage,
		"store_url":        storeURL,
	})
}

// cmpVersion compares dotted numeric versions ("1.2.0"), ignoring any "+build"
// suffix. Returns -1 if a<b, 0 if equal, 1 if a>b. Missing parts count as 0.
func cmpVersion(a, b string) int {
	pa, pb := splitVersion(a), splitVersion(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	v = strings.SplitN(v, "+", 2)[0] // drop "+build" metadata
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		out = append(out, n)
	}
	return out
}

// Get returns the app settings. GET /api/v1/admin/settings
func (h *SettingHandler) Get(c *fiber.Ctx) error {
	s, err := h.settings.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load settings")
	}
	s.SmtpPasswordSet = s.SmtpPassword != ""
	s.NexmoSecretSet = s.NexmoAPISecret != ""
	s.SmsExpertPasswordSet = s.SmsExpertPassword != ""
	s.CaptchaSecretSet = s.CaptchaSecret != ""
	s.AnthropicKeySet = s.AnthropicAPIKey != ""
	s.GeminiKeySet = s.GeminiAPIKey != ""
	s.VoyageKeySet = s.VoyageAPIKey != ""
	s.RazorpaySecretSet = s.RazorpayKeySecret != ""
	s.RazorpayWebhookSet = s.RazorpayWebhookSecret != ""
	s.RazorpayTestSecretSet = s.RazorpayTestKeySecret != ""
	s.RazorpayTestWebhookSet = s.RazorpayTestWebhookSecret != ""
	s.FcmConfigured = s.FcmServiceAccount != ""
	s.WhatsappTokenSet = s.WhatsappToken != ""
	return c.JSON(fiber.Map{"success": true, "settings": s})
}

// Update saves the app settings. PUT /api/v1/admin/settings
func (h *SettingHandler) Update(c *fiber.Ctx) error {
	var req settingRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	s, err := h.settings.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load settings")
	}

	req.AppName = strings.TrimSpace(req.AppName)
	if req.AppName != "" {
		s.AppName = req.AppName
	}
	s.SupportEmail = strings.TrimSpace(req.SupportEmail)
	s.SupportWhatsApp = digitsOnly(req.SupportWhatsApp) // wa.me needs bare digits
	s.LogoURL = strings.TrimSpace(req.LogoURL)
	s.EmailNotifications = req.EmailNotifications
	s.AutoApproveAnswers = req.AutoApproveAnswers
	s.MaintenanceMode = req.MaintenanceMode
	s.MaintenanceWeb = req.MaintenanceWeb
	s.MaintenanceMobile = req.MaintenanceMobile
	s.MaintenanceTitle = strings.TrimSpace(req.MaintenanceTitle)
	s.MaintenanceMessage = strings.TrimSpace(req.MaintenanceMessage)
	s.LatestAppVersion = strings.TrimSpace(req.LatestAppVersion)
	s.MinAppVersion = strings.TrimSpace(req.MinAppVersion)
	s.ForceUpdate = req.ForceUpdate
	s.UpdateMessage = strings.TrimSpace(req.UpdateMessage)
	s.AndroidStoreURL = strings.TrimSpace(req.AndroidStoreURL)
	s.IosStoreURL = strings.TrimSpace(req.IosStoreURL)
	if sa := strings.TrimSpace(req.FcmServiceAccount); sa != "" {
		// Validate the uploaded service account before storing it.
		if _, err := fcm.NewFromJSON(sa); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid service account JSON: "+err.Error())
		}
		s.FcmServiceAccount = sa
	}
	s.GoogleSsoEnabled = req.GoogleSsoEnabled
	s.GoogleClientID = strings.TrimSpace(req.GoogleClientID)

	// SMTP.
	s.SmtpEnabled = req.SmtpEnabled
	s.SmtpHost = strings.TrimSpace(req.SmtpHost)
	s.SmtpPort = strings.TrimSpace(req.SmtpPort)
	s.SmtpUser = strings.TrimSpace(req.SmtpUser)
	s.SmtpFrom = strings.TrimSpace(req.SmtpFrom)
	s.SmtpFromName = strings.TrimSpace(req.SmtpFromName)
	if strings.TrimSpace(req.SmtpPassword) != "" {
		s.SmtpPassword = req.SmtpPassword // only overwrite when a new one is given
	}

	// Error alerting.
	s.ErrorAlertsEnabled = req.ErrorAlertsEnabled
	s.AlertEmail = strings.TrimSpace(req.AlertEmail)

	// SMS.
	s.SmsEnabled = req.SmsEnabled
	if p := strings.TrimSpace(req.SmsProvider); p != "" {
		s.SmsProvider = p
	}
	if cc := strings.TrimSpace(req.SmsCountryCode); cc != "" {
		s.SmsCountryCode = cc
	}
	s.NexmoAPIKey = strings.TrimSpace(req.NexmoAPIKey)
	s.NexmoFrom = strings.TrimSpace(req.NexmoFrom)
	if strings.TrimSpace(req.NexmoAPISecret) != "" {
		s.NexmoAPISecret = req.NexmoAPISecret
	}
	s.SmsExpertAPIURL = strings.TrimSpace(req.SmsExpertAPIURL)
	s.SmsExpertUser = strings.TrimSpace(req.SmsExpertUser)
	s.SmsExpertSender = strings.TrimSpace(req.SmsExpertSender)
	s.SmsExpertRoute = strings.TrimSpace(req.SmsExpertRoute)
	s.SmsExpertType = strings.TrimSpace(req.SmsExpertType)
	if strings.TrimSpace(req.SmsExpertPassword) != "" {
		s.SmsExpertPassword = req.SmsExpertPassword
	}

	// Test OTP. The code must be 4–8 digits when the feature is on.
	s.TestOtpEnabled = req.TestOtpEnabled
	s.TestOtpPhones = strings.TrimSpace(req.TestOtpPhones)
	code := strings.TrimSpace(req.TestOtpCode)
	if code != "" {
		if len(code) < 4 || len(code) > 8 || strings.Trim(code, "0123456789") != "" {
			return fiber.NewError(fiber.StatusBadRequest, "test OTP must be 4–8 digits")
		}
	}
	s.TestOtpCode = code
	if s.TestOtpEnabled && (s.TestOtpCode == "" || s.TestOtpPhones == "") {
		return fiber.NewError(fiber.StatusBadRequest,
			"to enable the test OTP, set both the code and at least one phone number")
	}

	// WhatsApp (parents' daily report). Token is write-only.
	s.WhatsappEnabled = req.WhatsappEnabled
	s.WhatsappPhoneID = strings.TrimSpace(req.WhatsappPhoneID)
	s.WhatsappTemplate = strings.TrimSpace(req.WhatsappTemplate)
	s.WhatsappTemplateLang = strings.TrimSpace(req.WhatsappTemplateLang)
	if strings.TrimSpace(req.WhatsappToken) != "" {
		s.WhatsappToken = strings.TrimSpace(req.WhatsappToken)
	}

	// CAPTCHA.
	s.CaptchaEnabled = req.CaptchaEnabled
	if p := strings.TrimSpace(req.CaptchaProvider); p != "" {
		s.CaptchaProvider = p
	}
	s.CaptchaSiteKey = strings.TrimSpace(req.CaptchaSiteKey)
	if strings.TrimSpace(req.CaptchaSecret) != "" {
		s.CaptchaSecret = req.CaptchaSecret
	}

	// AI tutor. Keys are write-only (overwrite only when a new one is given).
	s.AIEnabled = req.AIEnabled
	if m := strings.TrimSpace(req.AnthropicModel); m != "" {
		s.AnthropicModel = m
	}
	if p := strings.TrimSpace(req.AnswersProvider); p == "claude" || p == "gemini" {
		s.AnswersProvider = p
	}
	if k := strings.TrimSpace(req.GeminiAPIKey); k != "" {
		s.GeminiAPIKey = k
	}
	if m := strings.TrimSpace(req.GeminiModel); m != "" {
		s.GeminiModel = m
	}
	if m := strings.TrimSpace(req.VoyageModel); m != "" {
		s.VoyageModel = m
	}
	// Embeddings backend (voyage | local) + local endpoint.
	if p := strings.TrimSpace(req.EmbeddingsProvider); p == "voyage" || p == "local" {
		s.EmbeddingsProvider = p
	}
	s.LocalEmbedURL = strings.TrimSpace(req.LocalEmbedURL)
	s.LocalEmbedModel = strings.TrimSpace(req.LocalEmbedModel)
	if strings.TrimSpace(req.AnthropicAPIKey) != "" {
		s.AnthropicAPIKey = strings.TrimSpace(req.AnthropicAPIKey)
	}
	if strings.TrimSpace(req.VoyageAPIKey) != "" {
		s.VoyageAPIKey = strings.TrimSpace(req.VoyageAPIKey)
	}

	// Razorpay. Key ids are not secrets; the secrets only overwrite when set.
	s.RazorpayEnabled = req.RazorpayEnabled
	s.RazorpayKeyID = strings.TrimSpace(req.RazorpayKeyID)
	if strings.TrimSpace(req.RazorpayKeySecret) != "" {
		s.RazorpayKeySecret = strings.TrimSpace(req.RazorpayKeySecret)
	}
	if strings.TrimSpace(req.RazorpayWebhookSecret) != "" {
		s.RazorpayWebhookSecret = strings.TrimSpace(req.RazorpayWebhookSecret)
	}
	s.RazorpayTestMode = req.RazorpayTestMode
	s.RazorpayTestKeyID = strings.TrimSpace(req.RazorpayTestKeyID)
	if strings.TrimSpace(req.RazorpayTestKeySecret) != "" {
		s.RazorpayTestKeySecret = strings.TrimSpace(req.RazorpayTestKeySecret)
	}
	if strings.TrimSpace(req.RazorpayTestWebhookSecret) != "" {
		s.RazorpayTestWebhookSecret = strings.TrimSpace(req.RazorpayTestWebhookSecret)
	}
	s.AutopayEnabled = req.AutopayEnabled
	s.CastEnabled = req.CastEnabled
	s.TimedTasksEnabled = req.TimedTasksEnabled

	// Referral program.
	s.ReferralEnabled = req.ReferralEnabled
	if req.ReferralRewardRupees >= 0 {
		s.ReferralRewardRupees = req.ReferralRewardRupees
	}
	s.ReferralShareMessage = strings.TrimSpace(req.ReferralShareMessage)
	s.ReferralLinkBase = strings.TrimSpace(req.ReferralLinkBase)

	// Firebase Analytics + Crashlytics (BigQuery export).
	s.AnalyticsEnabled = req.AnalyticsEnabled
	s.CrashlyticsEnabled = req.CrashlyticsEnabled
	s.BigQueryProjectID = strings.TrimSpace(req.BigQueryProjectID)
	s.AnalyticsDataset = strings.TrimSpace(req.AnalyticsDataset)
	s.CrashlyticsDataset = strings.TrimSpace(req.CrashlyticsDataset)
	s.CrashlyticsTable = strings.TrimSpace(req.CrashlyticsTable)
	s.CrashTestEnabled = req.CrashTestEnabled
	s.ChatSoundsEnabled = req.ChatSoundsEnabled
	s.ProfilePasswordEnabled = req.ProfilePasswordEnabled
	s.BiometricEnabled = req.BiometricEnabled

	if err := h.settings.Save(s); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to save settings")
	}
	s.SmtpPasswordSet = s.SmtpPassword != ""
	s.NexmoSecretSet = s.NexmoAPISecret != ""
	s.SmsExpertPasswordSet = s.SmsExpertPassword != ""
	s.CaptchaSecretSet = s.CaptchaSecret != ""
	s.AnthropicKeySet = s.AnthropicAPIKey != ""
	s.GeminiKeySet = s.GeminiAPIKey != ""
	s.VoyageKeySet = s.VoyageAPIKey != ""
	s.RazorpaySecretSet = s.RazorpayKeySecret != ""
	s.RazorpayWebhookSet = s.RazorpayWebhookSecret != ""
	s.RazorpayTestSecretSet = s.RazorpayTestKeySecret != ""
	s.RazorpayTestWebhookSet = s.RazorpayTestWebhookSecret != ""
	s.FcmConfigured = s.FcmServiceAccount != ""
	s.WhatsappTokenSet = s.WhatsappToken != ""
	return c.JSON(fiber.Map{"success": true, "settings": s})
}

// TestAI verifies the configured AI keys with a tiny Voyage + Claude round-trip.
// POST /api/v1/admin/settings/test-ai
func (h *SettingHandler) TestAI(c *fiber.Ctx) error {
	if h.aiProbe == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable,
			"AI tutoring is not available on this server (pgvector not installed)")
	}
	ctx, cancel := context.WithTimeout(c.Context(), 30*time.Second)
	defer cancel()
	if err := h.aiProbe(ctx); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "AI test failed — "+err.Error())
	}
	return c.JSON(fiber.Map{"success": true, "message": "AI keys are working (Voyage + Claude reachable)."})
}

type testEmailRequest struct {
	To string `json:"to"`
}

// TestEmail sends a test message using the current SMTP settings.
// POST /api/v1/admin/settings/test-email  { "to": "..." }
func (h *SettingHandler) TestEmail(c *fiber.Ctx) error {
	var req testEmailRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.To = strings.TrimSpace(req.To)
	if req.To == "" || !isEmail(req.To) {
		return fiber.NewError(fiber.StatusBadRequest, "a valid recipient email is required")
	}

	// A host must be saved first. The actual send happens via RabbitMQ: we
	// enqueue a Force job (ignores the "enabled" toggle) that the email worker
	// delivers, so testing is consistent with all other outgoing email.
	s, err := h.settings.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load settings")
	}
	if strings.TrimSpace(s.SmtpHost) == "" {
		return fiber.NewError(fiber.StatusBadRequest,
			"enter an SMTP host (and save) before sending a test")
	}

	body := `<p>This is a <strong>test email</strong> from your Vaha AI admin panel.</p>
<p>If you're reading this, your SMTP settings are working correctly. 🎉</p>`
	job := email.Job{
		To:      req.To,
		Subject: "Vaha AI — SMTP test email",
		HTML:    email.Wrap("It works!", body),
		Force:   true, // send even if the enabled toggle is off
		NoAlert: true, // a failed test shouldn't fire an error alert
	}
	if err := h.mailer.Enqueue(job); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not queue test email")
	}
	return c.JSON(fiber.Map{
		"success": true,
		"message": "Test email queued for " + req.To + " — it should arrive shortly.",
	})
}

// TestSMS enqueues a test SMS using the current SMS settings (Force job — sends
// even if the toggle is off, so credentials can be verified).
// POST /api/v1/admin/settings/test-sms  { "to": "..." }
func (h *SettingHandler) TestSMS(c *fiber.Ctx) error {
	var req testEmailRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.To = strings.TrimSpace(req.To)
	if !isPhone(req.To) {
		return fiber.NewError(fiber.StatusBadRequest, "a valid recipient phone number is required")
	}

	s, err := h.settings.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load settings")
	}
	configured := (s.SmsProvider == sms.ProviderSmsExpert && s.SmsExpertUser != "" && s.SmsExpertPassword != "") ||
		(s.SmsProvider != sms.ProviderSmsExpert && s.NexmoAPIKey != "")
	if !configured {
		return fiber.NewError(fiber.StatusBadRequest,
			"configure the SMS provider (and save) before sending a test")
	}

	job := sms.Job{
		To:    req.To,
		Text:  "Vaha AI: this is a test SMS. Your SMS settings are working!",
		Force: true,
	}
	if err := h.smser.Enqueue(job); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not queue test SMS")
	}
	return c.JSON(fiber.Map{
		"success": true,
		"message": "Test SMS queued for " + req.To + " — it should arrive shortly.",
	})
}

// digitsOnly keeps only 0-9 from s (e.g. "+91 98765 43210" -> "919876543210"),
// producing a wa.me-friendly WhatsApp number.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
