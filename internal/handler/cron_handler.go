package handler

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/scheduler"
	"github.com/gofiber/fiber/v2"
)

// CronHandler lets the admin list background jobs, toggle them, and run one now.
type CronHandler struct {
	crons *repository.CronRepository
	sched *scheduler.Scheduler
}

// NewCronHandler builds a CronHandler.
func NewCronHandler(crons *repository.CronRepository, sched *scheduler.Scheduler) *CronHandler {
	return &CronHandler{crons: crons, sched: sched}
}

// List returns all cron jobs. GET /api/v1/admin/crons
func (h *CronHandler) List(c *fiber.Ctx) error {
	jobs, err := h.crons.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load cron jobs")
	}
	return c.JSON(fiber.Map{"success": true, "jobs": jobs})
}

type cronToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// Toggle enables/disables a cron job. PUT /api/v1/admin/crons/:key { "enabled": true }
func (h *CronHandler) Toggle(c *fiber.Ctx) error {
	key := c.Params("key")
	var req cronToggleRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.crons.SetEnabled(key, req.Enabled); err != nil {
		if err == repository.ErrNotFound {
			return fiber.NewError(fiber.StatusNotFound, "cron job not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update cron job")
	}
	return c.JSON(fiber.Map{"success": true})
}

// Run executes a cron job immediately (ignoring its schedule/enabled gate).
// POST /api/v1/admin/crons/:key/run
func (h *CronHandler) Run(c *fiber.Ctx) error {
	key := c.Params("key")
	if h.sched == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "scheduler unavailable")
	}
	result, found := h.sched.RunNow(key)
	if !found {
		return fiber.NewError(fiber.StatusNotFound, "cron job not found")
	}
	return c.JSON(fiber.Map{"success": true, "result": result})
}
