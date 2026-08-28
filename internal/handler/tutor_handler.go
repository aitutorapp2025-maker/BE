package handler

import (
	"context"
	"strings"
	"time"

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
	// Block the AI tutor when AutoPay is required but off: a trial that never
	// enabled it, or a student who DELETED their mandate (paid → expired on the
	// cancellation webhook). Prompt them to (re-)enable AutoPay to continue.
	if h.autopay() && !st.AutopayActive &&
		(st.PayStatus == "trial" || st.PayStatus == "expired") {
		msg := "Enable AutoPay to start your free trial."
		if st.PayStatus == "expired" {
			msg = "AutoPay is turned off. Please enable AutoPay to keep using the AI tutor."
		}
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"success":       false,
			"error":         msg,
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

	// Every real AI answer is charged — including general-knowledge answers
	// when no textbook is indexed for the class (the AI cost is the same).
	balance = st.Credits
	if strings.TrimSpace(result.Answer) != "" {
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

// AskStream is the streaming variant of Ask (Server-Sent Events): the tutor's
// answer is emitted token-by-token so words appear as they're generated.
// POST /api/v1/student/ask/stream  (signed + end-to-end encrypted: the request
// is decrypted by DecryptRequest and each SSE frame is an AES-GCM envelope).
//
// All gating (auth, autopay, credits) happens up front and can still return a
// normal JSON error. Once streaming starts the body is a sequence of SSE events:
//
//	data: {"type":"delta","text":"..."}   (repeated as the answer generates)
//	data: {"type":"done","grounded":true,"sources":[...],"credits":N}
//	data: {"type":"error","message":"..."}   (on a mid-stream failure)
func (h *TutorHandler) AskStream(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	var req askRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		return fiber.NewError(fiber.StatusBadRequest, "question is required")
	}

	st, err := h.students.FindByID(studentID)
	if err != nil {
		return notFoundOrInternal(err, "student")
	}

	// Same gates as Ask — resolved before any streaming so they can 402/JSON.
	// Block the AI tutor when AutoPay is required but off: a trial that never
	// enabled it, or a student who DELETED their mandate (paid → expired on the
	// cancellation webhook). Prompt them to (re-)enable AutoPay to continue.
	if h.autopay() && !st.AutopayActive &&
		(st.PayStatus == "trial" || st.PayStatus == "expired") {
		msg := "Enable AutoPay to start your free trial."
		if st.PayStatus == "expired" {
			msg = "AutoPay is turned off. Please enable AutoPay to keep using the AI tutor."
		}
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"success":       false,
			"error":         msg,
			"needs_autopay": true,
		})
	}
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

	// Capture everything the stream writer needs — the fiber ctx must not be
	// touched inside the writer (it runs after this handler returns).
	sc := service.StudentContext{
		Class:            st.StudentClass,
		Medium:           st.Medium,
		Board:            st.Board,
		Group:            st.StudentGroup,
		TeachingLanguage: st.TeachingLanguage,
	}
	startBalance := st.Credits
	tutor := h.tutor
	credits := h.credits

	return sseStream(c, func(writeEvent func(v any)) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		result, err := tutor.AskStream(ctx, question, sc, func(delta string) {
			writeEvent(map[string]any{"type": "delta", "text": delta})
		})
		if err != nil {
			writeEvent(map[string]string{
				"type":    "error",
				"message": "Sorry, I couldn't answer that right now. Please try again.",
			})
			return
		}
		// Every real AI answer is charged — grounded or general (same rule as
		// Ask; the AI cost is the same either way).
		bal := startBalance
		if strings.TrimSpace(result.Answer) != "" {
			if nb, err := credits.Charge(studentID, service.ActionAskText); err == nil {
				bal = nb
			}
		}
		writeEvent(map[string]any{
			"type":     "done",
			"grounded": result.Grounded,
			"sources":  result.Sources,
			"credits":  bal,
		})
	})
}
