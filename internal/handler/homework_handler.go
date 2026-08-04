package handler

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"

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

type syncRequest struct {
	Since int64 `json:"since"` // unix ms of the client's last sync (0 = full)
}

// Sync returns the homeworks (with tasks) changed since the client's last sync,
// so the app keeps a local-first copy of the student's history and only pulls
// the delta. The response includes server_time (ms) which the client stores as
// the next `since`. POST (not GET) so the signed/E2E body carries `since`.
// POST /api/v1/student/sync  { "since": <ms> }
func (h *HomeworkHandler) Sync(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	var req syncRequest
	_ = c.BodyParser(&req)
	var since time.Time
	if req.Since > 0 {
		since = time.UnixMilli(req.Since)
	}
	list, err := h.hw.Sync(studentID, since)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "sync failed")
	}
	return c.JSON(fiber.Map{
		"success":     true,
		"homeworks":   list,
		"server_time": time.Now().UnixMilli(),
	})
}

// Teach returns a short grounded lesson for one task of a homework (Phase 3).
// POST /api/v1/student/homework/:id/tasks/:taskId/teach
func (h *HomeworkHandler) Teach(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	hwID, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	taskID, _ := strconv.ParseUint(c.Params("taskId"), 10, 64)
	if hwID == 0 || taskID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid ids")
	}
	res, err := h.hw.TeachTask(c.Context(), studentID, uint(hwID), uint(taskID))
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not teach this task: "+err.Error())
	}
	return c.JSON(fiber.Map{
		"success":  true,
		"lesson":   res.Answer,
		"grounded": res.Grounded,
	})
}

type doubtRequest struct {
	Question string `json:"question"`
}

// Doubt answers a follow-up question about one task (Phase 4 — doubt clearing).
// POST /api/v1/student/homework/:id/tasks/:taskId/doubt  { "question": "..." }
func (h *HomeworkHandler) Doubt(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	hwID, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	taskID, _ := strconv.ParseUint(c.Params("taskId"), 10, 64)
	if hwID == 0 || taskID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid ids")
	}
	var req doubtRequest
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.Question) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "a question is required")
	}
	res, err := h.hw.AskDoubt(c.Context(), studentID, uint(hwID), uint(taskID), req.Question)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not answer: "+err.Error())
	}
	return c.JSON(fiber.Map{"success": true, "answer": res.Answer, "grounded": res.Grounded})
}

type taskStatusRequest struct {
	Status string `json:"status"` // pending | done | skipped
}

// SetTaskStatus marks a task done/skipped/pending — the "execute one by one +
// skip" control. Returns the refreshed homework (with recomputed status).
// PATCH /api/v1/student/homework/:id/tasks/:taskId  { "status": "done" }
func (h *HomeworkHandler) SetTaskStatus(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	taskID, _ := strconv.ParseUint(c.Params("taskId"), 10, 64)
	if taskID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid task id")
	}
	var req taskStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	hw, err := h.hw.SetTaskStatus(studentID, uint(taskID), req.Status)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"success": true, "homework": hw})
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
