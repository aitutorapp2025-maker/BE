package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// AuditLogHandler lets an admin browse the audit trail (admin + student streams).
type AuditLogHandler struct {
	audits *repository.AuditLogRepository
}

// NewAuditLogHandler builds an AuditLogHandler.
func NewAuditLogHandler(audits *repository.AuditLogRepository) *AuditLogHandler {
	return &AuditLogHandler{audits: audits}
}

// List returns audit entries, filtered by actor_type (admin|student) and an
// optional from/to window (unix ms), paginated.
// GET /api/v1/admin/audit-logs?actor_type=student&page=1&page_size=50
func (h *AuditLogHandler) List(c *fiber.Ctx) error {
	actorType := strings.TrimSpace(c.Query("actor_type"))
	if actorType != "admin" && actorType != "student" {
		actorType = "" // all
	}
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(c.Query("page_size", "50"))
	if size < 1 || size > 200 {
		size = 50
	}
	var from, to time.Time
	if ms, err := strconv.ParseInt(c.Query("from"), 10, 64); err == nil && ms > 0 {
		from = time.UnixMilli(ms)
	}
	if ms, err := strconv.ParseInt(c.Query("to"), 10, 64); err == nil && ms > 0 {
		to = time.UnixMilli(ms)
	}

	logs, total, err := h.audits.List(actorType, from, to, size, (page-1)*size)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load audit logs")
	}
	return c.JSON(fiber.Map{
		"success":   true,
		"logs":      logs,
		"total":     total,
		"page":      page,
		"page_size": size,
	})
}

// Get returns one audit entry with its full request/response payloads.
// GET /api/v1/admin/audit-logs/:id
func (h *AuditLogHandler) Get(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	log, err := h.audits.FindByID(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "audit entry not found")
	}
	return c.JSON(fiber.Map{"success": true, "log": log})
}
