package handler

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// DashboardHandler serves the admin dashboard overview (aggregate stats +
// recent students), computed server-side.
type DashboardHandler struct {
	repo *repository.DashboardRepository
}

// NewDashboardHandler builds a DashboardHandler.
func NewDashboardHandler(repo *repository.DashboardRepository) *DashboardHandler {
	return &DashboardHandler{repo: repo}
}

// Get returns the dashboard overview.
//
// GET /api/v1/admin/dashboard
func (h *DashboardHandler) Get(c *fiber.Ctx) error {
	stats, err := h.repo.Stats()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load dashboard stats")
	}
	recent, err := h.repo.RecentStudents(5)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load recent students")
	}
	return c.JSON(fiber.Map{
		"success":         true,
		"stats":           stats,
		"recent_students": recent,
	})
}
