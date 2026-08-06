package handler

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// StudentPaymentHandler serves the signed-in student's own payment history
// (plan grants + recharges) for the in-app Billing screen — derived from the
// credit ledger's money-in rows.
type StudentPaymentHandler struct {
	credits *repository.CreditRepository
}

// NewStudentPaymentHandler builds a StudentPaymentHandler.
func NewStudentPaymentHandler(credits *repository.CreditRepository) *StudentPaymentHandler {
	return &StudentPaymentHandler{credits: credits}
}

// List returns the student's payment history, newest first.
// GET /api/v1/student/payments  (Bearer student JWT)
func (h *StudentPaymentHandler) List(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	rows, err := h.credits.PaymentsForStudent(studentID, 50)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load payments")
	}
	out := make([]fiber.Map, 0, len(rows))
	for _, r := range rows {
		status := "success"
		if r.Kind == "autopay_failed" {
			status = "failed"
		}
		out = append(out, fiber.Map{
			"id":            r.ID,
			"amount_rupees": r.RevenuePaise / 100,
			"credits":       r.Credits,
			"kind":          r.Kind, // grant | recharge | subscription | autopay_setup | autopay_failed
			"status":        status, // success | failed
			"note":          r.Note,
			"created_at":    r.CreatedAt,
		})
	}
	return c.JSON(fiber.Map{"success": true, "payments": out})
}
