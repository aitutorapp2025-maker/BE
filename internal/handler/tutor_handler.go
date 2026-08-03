package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// TutorHandler answers a signed-in student's textbook question (RAG). It reads
// the student's class / board / medium / group from their stored profile so the
// retrieval is scoped correctly — the client only sends the question. Each
// answer is metered against the student's AI credit balance.
type TutorHandler struct {
	tutor    *service.TutorService
	students *repository.StudentRepository
	credits  *service.CreditService
	autopay  service.AutopayEnabledFunc
}

// NewTutorHandler builds a TutorHandler. The autopay func gates the trial
// mandate requirement behind the admin toggle.
func NewTutorHandler(tutor *service.TutorService, students *repository.StudentRepository, credits *service.CreditService, autopay service.AutopayEnabledFunc) *TutorHandler {
	return &TutorHandler{tutor: tutor, students: students, credits: credits, autopay: autopay}
}

type askRequest struct {
	Question string `json:"question"`
}

// Ask answers the student's question from their class's textbooks.
// POST /api/v1/student/ask  { "question": "..." }  (signed + encrypted)
func (h *TutorHandler) Ask(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	var req askRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Question) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "question is required")
	}

	st, err := h.students.FindByID(studentID)
	if err != nil {
		return notFoundOrInternal(err, "student")
	}

	// Trial requires autopay (only when the admin toggle is on). If the student
	// is on trial but hasn't enabled (or has deleted) their UPI-AutoPay mandate,
	// they can't use the trial — prompt them to enable it. Paid students are
	// unaffected; if the admin disabled autopay, trials work without a mandate.
	if h.autopay() && st.PayStatus == "trial" && !st.AutopayActive {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"success":       false,
			"error":         "Enable autopay to start your free trial.",
			"needs_autopay": true,
		})
	}

	// Credit gate: check the balance up front so we don't pay for an AI call the
	// student can't afford. (Charged only after a successful answer, below.)
	ok, balance, err := h.credits.CanAfford(studentID, service.ActionAskText)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not check your credits")
	}
	if !ok {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"success": false,
			"error":   "You're out of credits. Please recharge to keep learning.",
			"credits": balance,
		})
	}

	result, err := h.tutor.Ask(c.Context(), req.Question, service.StudentContext{
		Class:            st.StudentClass,
		Medium:           st.Medium,
		Board:            st.Board,
		Group:            st.StudentGroup,
		TeachingLanguage: st.TeachingLanguage,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not answer right now, please try again")
	}

	// Charge only a genuine, grounded answer. If nothing was indexed for the
	// class (not the student's fault) we don't bill the credit.
	balance = st.Credits
	if result.Grounded {
		if nb, err := h.credits.Charge(studentID, service.ActionAskText); err == nil {
			balance = nb
		}
	}
	return c.JSON(fiber.Map{
		"success":  true,
		"answer":   result.Answer,
		"grounded": result.Grounded,
		"sources":  result.Sources,
		"credits":  balance,
	})
}
