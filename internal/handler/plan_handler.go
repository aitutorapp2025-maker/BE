package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// PlanHandler exposes CRUD endpoints for subscription plans (admin only).
type PlanHandler struct {
	plans *repository.PlanRepository
}

// NewPlanHandler builds a PlanHandler.
func NewPlanHandler(plans *repository.PlanRepository) *PlanHandler {
	return &PlanHandler{plans: plans}
}

type planRequest struct {
	Name         string   `json:"name"`
	PriceRupees  int      `json:"price_rupees"`
	MrpRupees    *int     `json:"mrp_rupees"`
	DurationDays int      `json:"duration_days"`
	Tagline      string   `json:"tagline"`
	Features     []string `json:"features"`
	BestValue    bool     `json:"best_value"`
}

// List returns all plans. GET /api/v1/admin/plans
func (h *PlanHandler) List(c *fiber.Ctx) error {
	plans, err := h.plans.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load plans")
	}
	return c.JSON(fiber.Map{"success": true, "plans": plans})
}

// Get returns a single plan. GET /api/v1/admin/plans/:id
func (h *PlanHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	p, err := h.plans.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "plan")
	}
	return c.JSON(fiber.Map{"success": true, "plan": p})
}

// Create adds a plan. POST /api/v1/admin/plans
func (h *PlanHandler) Create(c *fiber.Ctx) error {
	req, err := parsePlanBody(c)
	if err != nil {
		return err
	}
	p := &model.Plan{
		Name: req.Name, PriceRupees: req.PriceRupees, MrpRupees: req.MrpRupees,
		DurationDays: req.DurationDays, Tagline: req.Tagline,
		Features: req.Features, BestValue: req.BestValue,
	}
	if err := h.plans.Create(p); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create plan")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "plan": p})
}

// Update edits a plan. PUT /api/v1/admin/plans/:id
func (h *PlanHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	p, err := h.plans.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "plan")
	}
	req, err := parsePlanBody(c)
	if err != nil {
		return err
	}
	p.Name = req.Name
	p.PriceRupees = req.PriceRupees
	p.MrpRupees = req.MrpRupees
	p.DurationDays = req.DurationDays
	p.Tagline = req.Tagline
	p.Features = req.Features
	p.BestValue = req.BestValue
	if err := h.plans.Update(p); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update plan")
	}
	return c.JSON(fiber.Map{"success": true, "plan": p})
}

// Delete removes a plan. DELETE /api/v1/admin/plans/:id
func (h *PlanHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.plans.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete plan")
	}
	return c.JSON(fiber.Map{"success": true})
}

func parsePlanBody(c *fiber.Ctx) (*planRequest, error) {
	var req planRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	if req.Features == nil {
		req.Features = []string{}
	}
	return &req, nil
}
