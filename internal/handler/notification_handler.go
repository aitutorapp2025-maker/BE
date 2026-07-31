package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/fcm"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// NotificationHandler lets an admin broadcast an FCM push to all customers or a
// selected set of students.
type NotificationHandler struct {
	devices *repository.DeviceTokenRepository
	push    fcm.Pusher
}

// NewNotificationHandler builds a NotificationHandler.
func NewNotificationHandler(devices *repository.DeviceTokenRepository, push fcm.Pusher) *NotificationHandler {
	return &NotificationHandler{devices: devices, push: push}
}

type sendNotificationRequest struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	StudentIDs []uint `json:"student_ids"` // empty = all customers
}

// Send delivers a push to all customers (empty student_ids) or the chosen ones.
// POST /api/v1/admin/notifications/send
func (h *NotificationHandler) Send(c *fiber.Ctx) error {
	var req sendNotificationRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Body = strings.TrimSpace(req.Body)
	if req.Title == "" || req.Body == "" {
		return fiber.NewError(fiber.StatusBadRequest, "title and message are required")
	}
	if !h.push.Enabled() {
		return fiber.NewError(fiber.StatusServiceUnavailable,
			"push is not configured — add the Firebase service account (FCM_CREDENTIALS_FILE)")
	}

	var (
		tokens []string
		err    error
	)
	if len(req.StudentIDs) == 0 {
		tokens, err = h.devices.AllTokens()
	} else {
		tokens, err = h.devices.TokensForStudents(req.StudentIDs)
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load device tokens")
	}
	if len(tokens) == 0 {
		return c.JSON(fiber.Map{"success": true, "sent": 0, "devices": 0,
			"message": "No devices registered for the selected recipients."})
	}

	sent, _ := h.push.SendToTokens(c.Context(), tokens, req.Title, req.Body,
		map[string]string{"type": "admin_broadcast"})
	return c.JSON(fiber.Map{
		"success": true,
		"sent":    sent,
		"devices": len(tokens),
		"message": "Notification sent.",
	})
}
