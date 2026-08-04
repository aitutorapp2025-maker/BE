package handler

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// HomeworkHandler handles a student uploading a homework photo, which the AI
// reads and splits into learning tasks.
type HomeworkHandler struct {
	hw *service.HomeworkService
}

// NewHomeworkHandler builds a HomeworkHandler.
func NewHomeworkHandler(hw *service.HomeworkService) *HomeworkHandler {
	return &HomeworkHandler{hw: hw}
}

type uploadHomeworkRequest struct {
	Image     string `json:"image"`      // base64-encoded image bytes (no data: prefix)
	MediaType string `json:"media_type"` // image/jpeg | image/png | application/pdf
}

// Upload accepts a homework image, has the AI read + split it, and returns the
// stored homework with its tasks.
// POST /api/v1/student/homework  (signed + encrypted)
func (h *HomeworkHandler) Upload(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	var req uploadHomeworkRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	// Tolerate a data URL prefix if the client sent one.
	if i := strings.Index(req.Image, ","); strings.HasPrefix(req.Image, "data:") && i > 0 {
		req.Image = req.Image[i+1:]
	}
	bytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.Image))
	if err != nil || len(bytes) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid image data")
	}
	hw, err := h.hw.CreateFromImage(c.Context(), studentID, bytes, req.MediaType)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not read the homework: "+err.Error())
	}
	return c.JSON(fiber.Map{"success": true, "homework": hw})
}

// List returns the student's homeworks (newest first).
// GET /api/v1/student/homework
func (h *HomeworkHandler) List(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	list, err := h.hw.List(studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load homeworks")
	}
	return c.JSON(fiber.Map{"success": true, "homeworks": list})
}

// Get returns one homework (with tasks) scoped to the student.
// GET /api/v1/student/homework/:id
func (h *HomeworkHandler) Get(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	id, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	if id == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid homework id")
	}
	hw, err := h.hw.Get(uint(id), studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "homework not found")
	}
	return c.JSON(fiber.Map{"success": true, "homework": hw})
}
