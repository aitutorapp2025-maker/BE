package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/fcm"
	"github.com/gofiber/fiber/v2"
)

// NotificationHandler lets an admin broadcast an FCM push to all customers or a
// selected set of students. The send is queued on RabbitMQ and delivered by the
// push worker, so the HTTP request returns immediately.
type NotificationHandler struct {
	push *fcm.Publisher
}

// NewNotificationHandler builds a NotificationHandler.
func NewNotificationHandler(push *fcm.Publisher) *NotificationHandler {
	return &NotificationHandler{push: push}
}

type sendNotificationRequest struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	Image      string `json:"image"`       // optional picture URL shown in the notification
	StudentIDs []uint `json:"student_ids"` // empty = all customers
}

// Send queues a push to all customers (empty student_ids) or the chosen ones.
// The push worker resolves the device tokens, delivers via FCM and prunes any
// stale tokens.
//
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
			"push is not configured — add the Firebase service account in Settings")
	}

	if err := h.push.Enqueue(fcm.PushJob{
		Title:      req.Title,
		Body:       req.Body,
		Image:      strings.TrimSpace(req.Image),
		StudentIDs: req.StudentIDs,
	}); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not queue the notification")
	}
	return c.JSON(fiber.Map{
		"success": true,
		"queued":  true,
		"message": "Notification queued for delivery.",
	})
}
