package handler

import (
	"strings"

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
}

// NewSettingHandler builds a SettingHandler.
func NewSettingHandler(settings *repository.SettingRepository, mailer *email.Publisher, smser *sms.Publisher) *SettingHandler {
	return &SettingHandler{settings: settings, mailer: mailer, smser: smser}
}

type settingRequest struct {
	AppName            string `json:"app_name"`
	SupportEmail       string `json:"support_email"`
	EmailNotifications bool   `json:"email_notifications"`
	AutoApproveAnswers bool   `json:"auto_approve_answers"`
	MaintenanceMode    bool   `json:"maintenance_mode"`

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
	s.EmailNotifications = req.EmailNotifications
	s.AutoApproveAnswers = req.AutoApproveAnswers
	s.MaintenanceMode = req.MaintenanceMode

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

	if err := h.settings.Save(s); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to save settings")
	}
	s.SmtpPasswordSet = s.SmtpPassword != ""
	s.NexmoSecretSet = s.NexmoAPISecret != ""
	s.SmsExpertPasswordSet = s.SmsExpertPassword != ""
	return c.JSON(fiber.Map{"success": true, "settings": s})
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
