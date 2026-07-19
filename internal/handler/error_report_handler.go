package handler

import (
	"fmt"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/alert"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/gofiber/fiber/v2"
)

// ErrorReportHandler receives client-side (frontend) error reports and emails an
// alert (throttled + gated on error-alerts being enabled, same as server errors).
type ErrorReportHandler struct {
	alerter *alert.Alerter
}

// NewErrorReportHandler builds an ErrorReportHandler.
func NewErrorReportHandler(alerter *alert.Alerter) *ErrorReportHandler {
	return &ErrorReportHandler{alerter: alerter}
}

type errorReportRequest struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
	Source  string `json:"source"` // e.g. "flutter-web", "flutter-android"
	URL     string `json:"url"`
}

// Report accepts a frontend error and raises an alert.
// POST /api/v1/errors  (public — the landing page is unauthenticated)
func (h *ErrorReportHandler) Report(c *fiber.Ctx) error {
	var req errorReportRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return fiber.NewError(fiber.StatusBadRequest, "message is required")
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "frontend"
	}

	// Throttle by a short signature so repeats of the same error dedupe.
	subject := fmt.Sprintf("Frontend error (%s): %s", source, truncate(firstLine(msg), 80))
	body := fmt.Sprintf(`<p>A client-side error was reported by the app.</p>
<table cellpadding="6" style="border-collapse:collapse;font-size:14px;">
  <tr><td style="color:#5E6B63;">Source</td><td><strong>%s</strong></td></tr>
  <tr><td style="color:#5E6B63;vertical-align:top;">URL</td><td>%s</td></tr>
  <tr><td style="color:#5E6B63;vertical-align:top;">Message</td><td>%s</td></tr>
</table>
<pre style="background:#F5F1E6;padding:12px;border-radius:8px;font-size:12px;white-space:pre-wrap;overflow:auto;">%s</pre>`,
		email.Escape(source), email.Escape(req.URL), email.Escape(truncate(msg, 1000)),
		email.Escape(truncate(req.Stack, 3000)))

	h.alerter.Notify(subject, body)
	return c.JSON(fiber.Map{"success": true})
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
