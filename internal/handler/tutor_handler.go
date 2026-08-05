package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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

// AskStream is the streaming variant of Ask (Server-Sent Events): the tutor's
// answer is emitted token-by-token so words appear as they're generated.
// POST /api/v1/student/ask/stream  (signed; not body-encrypted — SSE can't go
// through the buffering Encrypt middleware).
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
	if h.autopay() && st.PayStatus == "trial" && !st.AutopayActive {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"success":       false,
			"error":         "Enable autopay to start your free trial.",
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

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no") // don't let nginx buffer the stream

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		writeEvent := func(v any) bool {
			b, err := json.Marshal(v)
			if err != nil {
				return false
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return false // client disconnected
			}
			return w.Flush() == nil
		}

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
		// Charge only a genuine, grounded answer (same rule as Ask).
		bal := startBalance
		if result.Grounded {
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
	return nil
}
