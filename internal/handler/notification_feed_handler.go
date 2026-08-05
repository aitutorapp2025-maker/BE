package handler

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// NotificationFeedHandler serves the signed-in student's notification feed
// (broadcasts + notifications targeted at them), so the app can show a list and
// an unread count. The "read" state is tracked on the device, not here.
type NotificationFeedHandler struct {
	notifs *repository.NotificationRepository
}

// NewNotificationFeedHandler builds a NotificationFeedHandler.
func NewNotificationFeedHandler(notifs *repository.NotificationRepository) *NotificationFeedHandler {
	return &NotificationFeedHandler{notifs: notifs}
}

// List returns the student's recent notifications, newest first.
// GET /api/v1/student/notifications  (Bearer student JWT)
func (h *NotificationFeedHandler) List(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	items, err := h.notifs.FeedForStudent(studentID, 50)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load notifications")
	}
	return c.JSON(fiber.Map{"success": true, "notifications": items})
}
