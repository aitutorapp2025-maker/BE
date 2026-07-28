package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// TutorHandler answers a signed-in student's textbook question (RAG). It reads
// the student's class / board / medium / group from their stored profile so the
// retrieval is scoped correctly — the client only sends the question.
type TutorHandler struct {
	tutor    *service.TutorService
	students *repository.StudentRepository
}

// NewTutorHandler builds a TutorHandler.
func NewTutorHandler(tutor *service.TutorService, students *repository.StudentRepository) *TutorHandler {
	return &TutorHandler{tutor: tutor, students: students}
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
	return c.JSON(fiber.Map{
		"success":  true,
		"answer":   result.Answer,
		"grounded": result.Grounded,
		"sources":  result.Sources,
	})
}
