package handler

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/payment"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// Default credit rate when a student has no priced plan (Standard: 104 ÷ 699).
const defaultTopupRate = 104.0 / 699.0

// Custom top-up bounds (rupees).
const (
	minTopupRupees = 10
	maxTopupRupees = 100000
)

// TopupHandler handles one-time credit top-ups for students and admin CRUD of
// the credit packs shown when a student's balance hits 0.
type TopupHandler struct {
	client   *payment.Client
	credits  *service.CreditService
	students *repository.StudentRepository
	plans    *repository.PlanRepository
	packs    *repository.CreditPackRepository
	topups   *repository.CreditTopupRepository
}

// NewTopupHandler builds a TopupHandler.
func NewTopupHandler(
	client *payment.Client,
	credits *service.CreditService,
	students *repository.StudentRepository,
	plans *repository.PlanRepository,
	packs *repository.CreditPackRepository,
	topups *repository.CreditTopupRepository,
) *TopupHandler {
	return &TopupHandler{client: client, credits: credits, students: students,
		plans: plans, packs: packs, topups: topups}
}

// rate returns the credits-per-rupee used for a custom top-up: the student's
// plan rate (credits ÷ price), falling back to the Standard rate.
func (h *TopupHandler) rate(st *model.Student) float64 {
	if st != nil {
		if plans, err := h.plans.List(); err == nil {
			for _, p := range plans {
				if p.Name == st.Plan && p.PriceRupees > 0 && p.Credits > 0 {
					return float64(p.Credits) / float64(p.PriceRupees)
				}
			}
		}
	}
	return defaultTopupRate
}

// creditsFor converts a rupee amount to credits at the given rate (min 1).
func creditsFor(amountRupees int, rate float64) int {
	c := int(math.Round(float64(amountRupees) * rate))
	if c < 1 && amountRupees > 0 {
		c = 1
	}
	return c
}

// ── Student ──────────────────────────────────────────────────────────────────

// Packs lists the active credit packs plus the custom-amount rate and bounds, so
// the app can render the "Buy credits" options. GET /student/credit-packs
func (h *TopupHandler) Packs(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	st, _ := h.students.FindByID(studentID)
	packs, err := h.packs.List(true)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load packs")
	}
	rate := h.rate(st)
	out := make([]fiber.Map, 0, len(packs))
	for _, p := range packs {
		out = append(out, fiber.Map{
			"id": p.ID, "amount_rupees": p.AmountRupees, "credits": p.Credits,
		})
	}
	return c.JSON(fiber.Map{
		"success":          true,
		"enabled":          h.client.Enabled(),
		"packs":            out,
		"credits_per_rupee": rate,
		"min_custom":       minTopupRupees,
		"max_custom":       maxTopupRupees,
		"key_id":           h.keyID(),
	})
}

type topupRequest struct {
	PackID       uint `json:"pack_id"`
	AmountRupees int  `json:"amount_rupees"`
}

// CreateTopup creates a one-time Razorpay order for a pack or a custom amount and
// records a pending top-up. POST /student/credits/topup
func (h *TopupHandler) CreateTopup(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	if !h.client.Enabled() {
		return fiber.NewError(fiber.StatusServiceUnavailable, "online payments are not configured yet")
	}
	var req topupRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	st, err := h.students.FindByID(studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "account not found")
	}

	var amountRupees, credits int
	if req.PackID > 0 {
		p, err := h.packs.FindByID(req.PackID)
		if err != nil || !p.Active {
			return fiber.NewError(fiber.StatusBadRequest, "invalid pack")
		}
		amountRupees, credits = p.AmountRupees, p.Credits
	} else {
		amountRupees = req.AmountRupees
		if amountRupees < minTopupRupees || amountRupees > maxTopupRupees {
			return fiber.NewError(fiber.StatusBadRequest, "amount out of range")
		}
		credits = creditsFor(amountRupees, h.rate(st))
	}

	amountPaise := amountRupees * 100
	receipt := "topup_" + strconv.FormatUint(uint64(studentID), 10) + "_" +
		strconv.FormatInt(time.Now().Unix(), 10)
	notes := map[string]string{
		"student_id": strconv.FormatUint(uint64(studentID), 10),
		"credits":    strconv.Itoa(credits),
		"kind":       "credit_topup",
	}
	orderID, err := h.client.CreateOrder(amountPaise, receipt, notes)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not start payment: "+err.Error())
	}
	if err := h.topups.Create(&model.CreditTopup{
		StudentID: studentID, OrderID: orderID, AmountPaise: amountPaise,
		Credits: credits, Status: "created",
	}); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not record top-up")
	}

	email := st.Email
	if email == "" && st.Phone != "" {
		email = st.Phone + "@vahaai.com" // phone-OTP students have no email
	}
	return c.JSON(fiber.Map{
		"success":       true,
		"order_id":      orderID,
		"amount_rupees": amountRupees,
		"amount_paise":  amountPaise,
		"credits":       credits,
		"key_id":        h.keyID(),
		"name":          st.Name,
		"email":         email,
		"contact":       st.Phone,
	})
}

type verifyTopupRequest struct {
	OrderID   string `json:"order_id"`
	PaymentID string `json:"payment_id"`
	Signature string `json:"signature"`
}

// VerifyTopup verifies the Razorpay Checkout signature and grants the credits
// recorded for that order (idempotent). POST /student/credits/topup/verify
func (h *TopupHandler) VerifyTopup(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	var req verifyTopupRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.OrderID = strings.TrimSpace(req.OrderID)
	req.PaymentID = strings.TrimSpace(req.PaymentID)
	if !h.client.VerifyPaymentSignature(req.OrderID, req.PaymentID, strings.TrimSpace(req.Signature)) {
		return fiber.NewError(fiber.StatusBadRequest, "payment verification failed")
	}
	t, err := h.topups.FindByOrder(req.OrderID)
	if err != nil || t.StudentID != studentID {
		return fiber.NewError(fiber.StatusNotFound, "top-up not found")
	}
	if t.Status == "paid" { // idempotent: webhook or a retry already granted it
		bal, _ := h.credits.Balance(studentID)
		return c.JSON(fiber.Map{"success": true, "credits_added": 0, "balance": bal})
	}
	bal, err := h.credits.Grant(int(studentID), t.Credits, int64(t.AmountPaise),
		"recharge", "Razorpay "+req.PaymentID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not add credits")
	}
	t.PaymentID = req.PaymentID
	t.Status = "paid"
	_ = h.topups.Update(t)
	return c.JSON(fiber.Map{"success": true, "credits_added": t.Credits, "balance": bal})
}

func (h *TopupHandler) keyID() string {
	// The key id is safe to expose; read it from a throwaway config via the client.
	return h.client.KeyID()
}

// ── Admin: credit-pack CRUD ──────────────────────────────────────────────────

type creditPackRequest struct {
	AmountRupees int  `json:"amount_rupees"`
	Credits      int  `json:"credits"`
	Active       bool `json:"active"`
	Sort         int  `json:"sort"`
}

// AdminListPacks returns all packs (active + inactive). GET /admin/credit-packs
func (h *TopupHandler) AdminListPacks(c *fiber.Ctx) error {
	packs, err := h.packs.List(false)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load packs")
	}
	return c.JSON(fiber.Map{"success": true, "packs": packs})
}

// AdminCreatePack adds a pack. POST /admin/credit-packs
func (h *TopupHandler) AdminCreatePack(c *fiber.Ctx) error {
	req, err := parsePackBody(c)
	if err != nil {
		return err
	}
	p := &model.CreditPack{AmountRupees: req.AmountRupees, Credits: req.Credits,
		Active: req.Active, Sort: req.Sort}
	if err := h.packs.Create(p); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create pack")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "pack": p})
}

// AdminUpdatePack edits a pack. PUT /admin/credit-packs/:id
func (h *TopupHandler) AdminUpdatePack(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	p, err := h.packs.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "pack")
	}
	req, err := parsePackBody(c)
	if err != nil {
		return err
	}
	p.AmountRupees = req.AmountRupees
	p.Credits = req.Credits
	p.Active = req.Active
	p.Sort = req.Sort
	if err := h.packs.Update(p); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update pack")
	}
	return c.JSON(fiber.Map{"success": true, "pack": p})
}

// AdminDeletePack removes a pack. DELETE /admin/credit-packs/:id
func (h *TopupHandler) AdminDeletePack(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.packs.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete pack")
	}
	return c.JSON(fiber.Map{"success": true})
}

func parsePackBody(c *fiber.Ctx) (*creditPackRequest, error) {
	var req creditPackRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if req.AmountRupees <= 0 {
		return nil, fiber.NewError(fiber.StatusBadRequest, "amount must be positive")
	}
	if req.Credits <= 0 {
		return nil, fiber.NewError(fiber.StatusBadRequest, "credits must be positive")
	}
	return &req, nil
}
