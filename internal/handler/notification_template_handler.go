package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// NotificationTemplateHandler exposes admin CRUD for the editable wording of the
// automated notifications.
type NotificationTemplateHandler struct {
	repo *repository.NotificationTemplateRepository
}

// NewNotificationTemplateHandler builds a NotificationTemplateHandler.
func NewNotificationTemplateHandler(repo *repository.NotificationTemplateRepository) *NotificationTemplateHandler {
	return &NotificationTemplateHandler{repo: repo}
}

// List returns every editable template merged with its default + metadata
// (label, placeholders, defaults) so the admin UI can render and reset each one.
// GET /api/v1/admin/notification-templates
func (h *NotificationTemplateHandler) List(c *fiber.Ctx) error {
	rows, err := h.repo.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load templates")
	}
	byType := make(map[string]model.NotificationTemplate, len(rows))
	for _, r := range rows {
		byType[r.Type] = r
	}
	out := make([]fiber.Map, 0)
	for _, d := range model.DefaultNotificationTemplates() {
		title, body, active := d.Title, d.Body, true
		if r, ok := byType[d.Type]; ok {
			active = r.Active
			if strings.TrimSpace(r.Title) != "" {
				title = r.Title
			}
			if strings.TrimSpace(r.Body) != "" {
				body = r.Body
			}
		}
		out = append(out, fiber.Map{
			"type":          d.Type,
			"label":         d.Label,
			"title":         title,
			"body":          body,
			"active":        active,
			"placeholders":  d.Placeholders,
			"default_title": d.Title,
			"default_body":  d.Body,
		})
	}
	return c.JSON(fiber.Map{"success": true, "templates": out})
}

type notifTemplateRequest struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Active bool   `json:"active"`
}

// Update saves one template. PUT /api/v1/admin/notification-templates/:type
func (h *NotificationTemplateHandler) Update(c *fiber.Ctx) error {
	typ := strings.TrimSpace(c.Params("type"))
	if dt, _ := model.DefaultNotifTemplate(typ); dt == "" {
		return fiber.NewError(fiber.StatusBadRequest, "unknown notification type")
	}
	var req notifTemplateRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	t, err := h.repo.Upsert(typ, strings.TrimSpace(req.Title),
		strings.TrimSpace(req.Body), req.Active)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to save template")
	}
	return c.JSON(fiber.Map{"success": true, "template": t})
}
