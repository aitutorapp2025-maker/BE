package handler

import (
	"fmt"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/captcha"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/sms"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
	"github.com/gofiber/fiber/v2"
)

// ContactHandler handles landing-page "Get in touch" submissions.
type ContactHandler struct {
	contacts *repository.ContactRepository
	settings *repository.SettingRepository
	mailer   *email.Publisher
	smser    *sms.Publisher
	log      *logger.Logger
}

// NewContactHandler builds a ContactHandler.
func NewContactHandler(contacts *repository.ContactRepository, settings *repository.SettingRepository, mailer *email.Publisher, smser *sms.Publisher, log *logger.Logger) *ContactHandler {
	return &ContactHandler{contacts: contacts, settings: settings, mailer: mailer, smser: smser, log: log}
}

type contactRequest struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Message      string `json:"message"`
	CaptchaToken string `json:"captcha_token"`
}

// Submit saves a public contact submission and emails the visitor a receipt.
// POST /api/v1/contact  (no auth)
func (h *ContactHandler) Submit(c *fiber.Ctx) error {
	var req contactRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	req.Message = strings.TrimSpace(req.Message)
	if req.Name == "" || req.Message == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name and message are required")
	}

	// CAPTCHA verification (bot protection) when enabled.
	if s, err := h.settings.Get(); err == nil && s.CaptchaEnabled && s.CaptchaSecret != "" {
		if err := captcha.Verify(c.Context(), s.CaptchaProvider, s.CaptchaSecret, req.CaptchaToken, c.IP()); err != nil {
			h.log.Warnf("contact: captcha failed from %s: %v", c.IP(), err)
			return fiber.NewError(fiber.StatusBadRequest, "Please complete the verification to prove you're human.")
		}
	}

	msg := &model.ContactMessage{
		Name:    req.Name,
		Email:   req.Email,
		Message: req.Message,
		Status:  "new",
	}
	if err := h.contacts.Create(msg); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not save your message")
	}

	// Queue a confirmation via the channel the visitor used: email if they gave
	// an email address, SMS if they gave a phone number. Publishing to RabbitMQ
	// keeps the response instant; the workers deliver.
	emailed, smsed := false, false
	switch {
	case isEmail(req.Email):
		if h.mailer.Enabled() {
			job := email.Job{
				To:      req.Email,
				Subject: "We got your message — Vaha AI",
				HTML:    confirmationHTML(req.Name),
			}
			if err := h.mailer.Enqueue(job); err != nil {
				h.log.Errorf("contact: failed to queue email to %s: %v", req.Email, err)
			} else {
				emailed = true
			}
		}
	case isPhone(req.Email):
		if h.smser.Enabled() {
			name := req.Name
			if name == "" {
				name = "there"
			}
			job := sms.Job{
				To: req.Email,
				Text: fmt.Sprintf(
					"Hi %s, thanks for contacting Vaha AI! We'll get back to you within a day.", name),
			}
			if err := h.smser.Enqueue(job); err != nil {
				h.log.Errorf("contact: failed to queue sms to %s: %v", req.Email, err)
			} else {
				smsed = true
			}
		}
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"emailed": emailed,
		"smsed":   smsed,
		"message": "Thanks! We'll get back to you within a day.",
	})
}

// List returns all submissions. GET /api/v1/admin/contacts
func (h *ContactHandler) List(c *fiber.Ctx) error {
	items, err := h.contacts.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load messages")
	}
	return c.JSON(fiber.Map{"success": true, "contacts": items})
}

// Get returns one submission and marks it read. GET /api/v1/admin/contacts/:id
func (h *ContactHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	m, err := h.contacts.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "message")
	}
	if m.Status == "new" {
		_ = h.contacts.MarkRead(id)
		m.Status = "read"
	}
	return c.JSON(fiber.Map{"success": true, "contact": m})
}

// Delete removes a submission. DELETE /api/v1/admin/contacts/:id
func (h *ContactHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.contacts.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete message")
	}
	return c.JSON(fiber.Map{"success": true})
}

func isEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	return at > 0 && strings.IndexByte(s[at+1:], '.') >= 0
}

// isPhone reports whether s looks like a phone number (10–15 digits, ignoring
// common separators and an optional leading +).
func isPhone(s string) bool {
	digits := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')':
			// allowed separators
		default:
			return false
		}
	}
	return digits >= 10 && digits <= 15
}

func confirmationHTML(name string) string {
	body := fmt.Sprintf(`<p>Hi %s,</p>
<p>We've received your message and a member of the Vaha AI team will get back to you within a day.</p>
<p>In the meantime, you can start a free 7-day trial anytime — no card required.</p>
<p style="margin-top:20px;color:#5E6B63;">— The Vaha AI team</p>`, email.Escape(name))
	return email.Wrap("Thanks for reaching out!", body)
}
