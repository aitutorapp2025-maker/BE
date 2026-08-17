package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/payment"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// PlanHandler exposes CRUD endpoints for subscription plans (admin only).
type PlanHandler struct {
	plans    *repository.PlanRepository
	razorpay *payment.Client
}

// NewPlanHandler builds a PlanHandler.
func NewPlanHandler(plans *repository.PlanRepository, razorpay *payment.Client) *PlanHandler {
	return &PlanHandler{plans: plans, razorpay: razorpay}
}

// syncRazorpayPlan creates a Razorpay plan for a paid tier in the CURRENTLY
// ACTIVE mode (live or test — Razorpay plan ids are mode-scoped) and stores it
// on the matching column. Since Razorpay plans are immutable, callers use this
// on create and whenever the price/duration changes. Returns an error the
// caller surfaces to the admin.
func (h *PlanHandler) syncRazorpayPlan(p *model.Plan) error {
	if p.IsTrial || p.PriceRupees <= 0 {
		return nil // free / trial plans have no Razorpay plan
	}
	id, err := h.razorpay.CreatePlan(p.Name, p.PriceRupees*100, payment.IntervalMonths(p.DurationDays))
	if err != nil {
		return err
	}
	p.SetRzpPlanID(h.razorpay.TestMode(), id)
	return nil
}

type planRequest struct {
	Name           string   `json:"name"`
	PriceRupees    int      `json:"price_rupees"`
	MrpRupees      *int     `json:"mrp_rupees"`
	DurationDays   int      `json:"duration_days"`
	Tagline        string   `json:"tagline"`
	Features       []string `json:"features"`
	BestValue      bool     `json:"best_value"`
	Credits        int      `json:"credits"`
	IsTrial        bool     `json:"is_trial"`
	RazorpayPlanID string   `json:"razorpay_plan_id"`
}

// List returns all plans. GET /api/v1/admin/plans
func (h *PlanHandler) List(c *fiber.Ctx) error {
	plans, err := h.plans.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load plans")
	}
	return c.JSON(fiber.Map{"success": true, "plans": plans})
}

// Public returns the plans for the landing page + student app (ordered by
// price). GET /api/v1/plans
func (h *PlanHandler) Public(c *fiber.Ctx) error {
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
		Features: req.Features, BestValue: req.BestValue, Credits: req.Credits,
		IsTrial: req.IsTrial, RazorpayPlanID: strings.TrimSpace(req.RazorpayPlanID),
	}
	// Auto-create the Razorpay plan for a paid tier in the active mode (unless
	// a live id was given manually).
	var warning string
	if p.RzpPlanID(h.razorpay.TestMode()) == "" {
		if err := h.syncRazorpayPlan(p); err != nil {
			warning = razorpayWarn(err)
		}
	}
	if err := h.plans.Create(p); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create plan")
	}
	resp := fiber.Map{"success": true, "plan": p}
	if warning != "" {
		resp["warning"] = warning
	}
	return c.Status(fiber.StatusCreated).JSON(resp)
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
	testMode := h.razorpay.TestMode()
	oldPrice, oldDuration := p.PriceRupees, p.DurationDays
	oldActive := p.RzpPlanID(testMode)
	p.Name = req.Name
	p.PriceRupees = req.PriceRupees
	p.MrpRupees = req.MrpRupees
	p.DurationDays = req.DurationDays
	p.Tagline = req.Tagline
	p.Features = req.Features
	p.BestValue = req.BestValue
	p.Credits = req.Credits
	p.IsTrial = req.IsTrial
	p.RazorpayPlanID = strings.TrimSpace(req.RazorpayPlanID)

	// Razorpay plans are immutable, so (re)create one whenever the price or
	// billing period changes, or when a paid tier has no id yet in the ACTIVE
	// mode. On a price change BOTH mode ids are stale — clear them so neither
	// mode can ever charge the old amount (the other mode's id is recreated
	// lazily on its next use).
	var warning string
	priceChanged := p.PriceRupees != oldPrice || p.DurationDays != oldDuration
	if priceChanged {
		p.RazorpayPlanID = ""
		p.RazorpayTestPlanID = ""
	}
	needsSync := !p.IsTrial && p.PriceRupees > 0 &&
		(priceChanged || p.RzpPlanID(testMode) == "")
	if needsSync {
		if err := h.syncRazorpayPlan(p); err != nil {
			// Creation failed. After a price change the ids MUST stay cleared (an
			// old id would keep charging the previous amount — the "changed ₹5→₹2
			// but checkout shows ₹5" bug); AutoPay fails cleanly ("not linked")
			// until the next save or subscribe retries the sync. With an
			// unchanged price, keep the previous link.
			if !priceChanged && p.RzpPlanID(testMode) == "" {
				p.SetRzpPlanID(testMode, oldActive)
			}
			warning = razorpayWarn(err)
		}
	}
	if err := h.plans.Update(p); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update plan")
	}
	resp := fiber.Map{"success": true, "plan": p}
	if warning != "" {
		resp["warning"] = warning
	}
	return c.JSON(resp)
}

func razorpayWarn(err error) string {
	return "Plan saved, but the Razorpay plan couldn't be created: " + err.Error()
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
