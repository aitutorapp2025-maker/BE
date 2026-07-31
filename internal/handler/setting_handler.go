package handler

import (
	"context"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
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
	LogoURL            string `json:"logo_url"`
	EmailNotifications bool   `json:"email_notifications"`
	AutoApproveAnswers bool   `json:"auto_approve_answers"`
	MaintenanceMode    bool   `json:"maintenance_mode"`

	MaintenanceWeb     bool   `json:"maintenance_web"`
	MaintenanceMobile  bool   `json:"maintenance_mobile"`
	MaintenanceTitle   string `json:"maintenance_title"`
	MaintenanceMessage string `json:"maintenance_message"`

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

	// CAPTCHA. Secret is write-only.
	CaptchaEnabled  bool   `json:"captcha_enabled"`
	CaptchaProvider string `json:"captcha_provider"`
	CaptchaSiteKey  string `json:"captcha_site_key"`
	CaptchaSecret   string `json:"captcha_secret"`

	// AI tutor. Keys are write-only: empty means "keep the existing one".
	AIEnabled       bool   `json:"ai_enabled"`
	AnthropicAPIKey string `json:"anthropic_api_key"`
	AnthropicModel  string `json:"anthropic_model"`
	VoyageAPIKey    string `json:"voyage_api_key"`
	VoyageModel     string `json:"voyage_model"`

	// Razorpay. Secrets are write-only; key_id is not a secret.
	RazorpayEnabled       bool   `json:"razorpay_enabled"`
	RazorpayKeyID         string `json:"razorpay_key_id"`
	RazorpayKeySecret     string `json:"razorpay_key_secret"`
	RazorpayWebhookSecret string `json:"razorpay_webhook_secret"`
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
	s.VoyageKeySet = s.VoyageAPIKey != ""
	s.RazorpaySecretSet = s.RazorpayKeySecret != ""
	s.RazorpayWebhookSet = s.RazorpayWebhookSecret != ""
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
	s.LogoURL = strings.TrimSpace(req.LogoURL)
	s.EmailNotifications = req.EmailNotifications
	s.AutoApproveAnswers = req.AutoApproveAnswers
	s.MaintenanceMode = req.MaintenanceMode
	s.MaintenanceWeb = req.MaintenanceWeb
	s.MaintenanceMobile = req.MaintenanceMobile
	s.MaintenanceTitle = strings.TrimSpace(req.MaintenanceTitle)
	s.MaintenanceMessage = strings.TrimSpace(req.MaintenanceMessage)

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
	if m := strings.TrimSpace(req.VoyageModel); m != "" {
		s.VoyageModel = m
	}
	if strings.TrimSpace(req.AnthropicAPIKey) != "" {
		s.AnthropicAPIKey = strings.TrimSpace(req.AnthropicAPIKey)
	}
	if strings.TrimSpace(req.VoyageAPIKey) != "" {
		s.VoyageAPIKey = strings.TrimSpace(req.VoyageAPIKey)
	}

	// Razorpay. key_id is not a secret; the two secrets only overwrite when set.
	s.RazorpayEnabled = req.RazorpayEnabled
	s.RazorpayKeyID = strings.TrimSpace(req.RazorpayKeyID)
	if strings.TrimSpace(req.RazorpayKeySecret) != "" {
		s.RazorpayKeySecret = strings.TrimSpace(req.RazorpayKeySecret)
	}
	if strings.TrimSpace(req.RazorpayWebhookSecret) != "" {
		s.RazorpayWebhookSecret = strings.TrimSpace(req.RazorpayWebhookSecret)
	}

	if err := h.settings.Save(s); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to save settings")
	}
	s.SmtpPasswordSet = s.SmtpPassword != ""
	s.NexmoSecretSet = s.NexmoAPISecret != ""
	s.SmsExpertPasswordSet = s.SmsExpertPassword != ""
	s.CaptchaSecretSet = s.CaptchaSecret != ""
	s.AnthropicKeySet = s.AnthropicAPIKey != ""
	s.VoyageKeySet = s.VoyageAPIKey != ""
	s.RazorpaySecretSet = s.RazorpayKeySecret != ""
	s.RazorpayWebhookSet = s.RazorpayWebhookSecret != ""
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
