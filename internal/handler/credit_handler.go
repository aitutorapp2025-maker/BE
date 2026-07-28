package handler

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// CreditHandler exposes the admin billing surface: top up a student's credits
// and view the profit & loss (revenue vs AI cost). Students never see P&L.
type CreditHandler struct {
	credits  *service.CreditService
	students *repository.StudentRepository
}

// NewCreditHandler builds a CreditHandler.
func NewCreditHandler(credits *service.CreditService, students *repository.StudentRepository) *CreditHandler {
	return &CreditHandler{credits: credits, students: students}
}

type rechargeRequest struct {
	Credits      int    `json:"credits"`
	AmountRupees int    `json:"amount_rupees"` // money received (for P&L revenue)
	Note         string `json:"note"`
	Kind         string `json:"kind"` // "recharge" (default) | "grant"
}

// Recharge tops up a student's credit balance and records the revenue.
// POST /api/v1/admin/students/:id/recharge
func (h *CreditHandler) Recharge(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if _, err := h.students.FindByID(id); err != nil {
		return notFoundOrInternal(err, "student")
	}
	var req rechargeRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if req.Credits <= 0 {
		return fiber.NewError(fiber.StatusBadRequest, "credits must be positive")
	}
	kind := req.Kind
	if kind != "grant" {
		kind = "recharge"
	}
	note := req.Note
	if note == "" {
		note = "Admin recharge"
	}
	balance, err := h.credits.Grant(int(id), req.Credits, int64(req.AmountRupees)*100, kind, note)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not add credits")
	}
	return c.JSON(fiber.Map{"success": true, "credits": balance})
}

// Ledger returns a student's recent credit history (admin).
// GET /api/v1/admin/students/:id/ledger
func (h *CreditHandler) Ledger(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	rows, err := h.credits.Recent(id, 50)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load credit history")
	}
	return c.JSON(fiber.Map{"success": true, "ledger": rows})
}

// PnL returns the aggregate profit & loss for the admin billing view.
// GET /api/v1/admin/billing
func (h *CreditHandler) PnL(c *fiber.Ctx) error {
	p, err := h.credits.Summary()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not compute profit & loss")
	}
	revenue := p.RevenuePaise / 100
	cost := p.AICostPaise / 100
	profit := revenue - cost
	margin := 0.0
	if p.RevenuePaise > 0 {
		margin = float64(p.RevenuePaise-p.AICostPaise) / float64(p.RevenuePaise) * 100
	}
	// The credit price list, so the admin can see what each AI action costs.
	prices := map[string]any{}
	for action, cost := range service.ActionCosts() {
		prices[action] = fiber.Map{
			"credits":       cost.Credits,
			"ai_cost_paise": cost.AICostPaise,
			"label":         cost.Label,
		}
	}
	return c.JSON(fiber.Map{
		"success":         true,
		"revenue_rupees":  revenue,
		"ai_cost_rupees":  cost,
		"profit_rupees":   profit,
		"margin_pct":      margin,
		"debit_count":     p.Debits,
		"paying_students": p.Students,
		"action_prices":   prices,
	})
}
