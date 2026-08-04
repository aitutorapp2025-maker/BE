package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

// maxUploadsPerDay caps homework uploads per student per day (abuse guard, on
// top of credit metering).
const maxUploadsPerDay = 30

// maxHomeworkBytes caps a decoded homework image/PDF (guards memory + cost).
const maxHomeworkBytes = 12 << 20 // 12 MB

// HomeworkHandler handles a student uploading a homework photo, which the AI
// reads and splits into learning tasks. AI actions are metered on credits and
// uploads are rate-limited.
type HomeworkHandler struct {
	hw      *service.HomeworkService
	credits *service.CreditService
	rdb     *redis.Client
}

// NewHomeworkHandler builds a HomeworkHandler.
func NewHomeworkHandler(hw *service.HomeworkService, credits *service.CreditService, rdb *redis.Client) *HomeworkHandler {
	return &HomeworkHandler{hw: hw, credits: credits, rdb: rdb}
}

// requireCredits returns a 402 error when the student can't afford the action.
func (h *HomeworkHandler) requireCredits(studentID uint, action string) error {
	ok, balance, err := h.credits.CanAfford(studentID, action)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not check your credits")
	}
	if !ok {
		return fiber.NewError(fiber.StatusPaymentRequired,
			fmt.Sprintf("You're out of credits (%d left). Please recharge to keep learning.", balance))
	}
	return nil
}

// rateLimitUpload increments a per-student per-day counter and returns a 429
// error once the daily upload cap is hit.
func (h *HomeworkHandler) rateLimitUpload(ctx context.Context, studentID uint) error {
	if h.rdb == nil {
		return nil
	}
	key := fmt.Sprintf("hw:up:%d:%s", studentID, time.Now().UTC().Format("20060102"))
	n, err := h.rdb.Incr(ctx, key).Result()
	if err != nil {
		return nil // Redis down — don't block the student
	}
	if n == 1 {
		_ = h.rdb.Expire(ctx, key, 24*time.Hour).Err()
	}
	if n > maxUploadsPerDay {
		return fiber.NewError(fiber.StatusTooManyRequests,
			"You've reached today's homework upload limit. Please try again tomorrow.")
	}
	return nil
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
	if len(bytes) > maxHomeworkBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge,
			"that image is too large — please use a smaller photo")
	}
	if err := h.rateLimitUpload(c.Context(), studentID); err != nil {
		return err
	}
	if err := h.requireCredits(studentID, service.ActionHomeworkRead); err != nil {
		return err
	}
	hw, err := h.hw.CreateFromImage(c.Context(), studentID, bytes, req.MediaType)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not read the homework: "+err.Error())
	}
	_, _ = h.credits.Charge(studentID, service.ActionHomeworkRead)
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

// Report returns the student's performance report (marks + progress, date-wise).
// GET /api/v1/student/report
func (h *HomeworkHandler) Report(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	report, err := h.hw.Report(studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not build the report")
	}
	return c.JSON(fiber.Map{"success": true, "report": report})
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
	if err := h.requireCredits(studentID, service.ActionTeach); err != nil {
		return err
	}
	res, cached, err := h.hw.TeachTask(c.Context(), studentID, uint(hwID), uint(taskID))
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not teach this task: "+err.Error())
	}
	// Only charge when we actually called the AI (a cached lesson is free).
	if !cached {
		_, _ = h.credits.Charge(studentID, service.ActionTeach)
	}
	return c.JSON(fiber.Map{
		"success":  true,
		"lesson":   res.Answer,
		"grounded": res.Grounded,
	})
}

// GenerateTest returns a short written test for the homework (Phase 5).
// POST /api/v1/student/homework/:id/test
func (h *HomeworkHandler) GenerateTest(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	hwID, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	if hwID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid homework id")
	}
	qs, err := h.hw.GenerateTest(c.Context(), studentID, uint(hwID))
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not create the test: "+err.Error())
	}
	return c.JSON(fiber.Map{"success": true, "questions": qs})
}

type gradeTestRequest struct {
	Questions []service.TestQuestion `json:"questions"`
	Answers   []string               `json:"answers"`    // typed answers or speech transcripts
	Image     string                 `json:"image"`      // base64 handwritten answer sheet (optional)
	MediaType string                 `json:"media_type"` // for the image
	Kind      string                 `json:"kind"`       // written | oral (text answers)
}

// GradeTest grades the student's answers — typed, or a handwritten photo read by
// Claude vision — and stores the result. POST /api/v1/student/homework/:id/test/grade
func (h *HomeworkHandler) GradeTest(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	hwID, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	if hwID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid homework id")
	}
	var req gradeTestRequest
	if err := c.BodyParser(&req); err != nil || len(req.Questions) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "questions are required")
	}
	gradeAction := service.ActionWrittenExam
	if req.Kind == "oral" {
		gradeAction = service.ActionOralExam
	}
	if err := h.requireCredits(studentID, gradeAction); err != nil {
		return err
	}

	var (
		test *model.HomeworkTest
		err  error
	)
	if strings.TrimSpace(req.Image) != "" {
		img := req.Image
		if i := strings.Index(img, ","); strings.HasPrefix(img, "data:") && i > 0 {
			img = img[i+1:]
		}
		bytes, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(img))
		if derr != nil || len(bytes) == 0 {
			return fiber.NewError(fiber.StatusBadRequest, "invalid image data")
		}
		test, err = h.hw.GradeWrittenImage(c.Context(), studentID, uint(hwID), req.Questions, bytes, req.MediaType)
	} else {
		test, err = h.hw.GradeWritten(c.Context(), studentID, uint(hwID), req.Questions, req.Answers, req.Kind)
	}
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not grade the test: "+err.Error())
	}
	_, _ = h.credits.Charge(studentID, gradeAction)
	return c.JSON(fiber.Map{"success": true, "test": test})
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
	if err := h.requireCredits(studentID, service.ActionDoubt); err != nil {
		return err
	}
	res, err := h.hw.AskDoubt(c.Context(), studentID, uint(hwID), uint(taskID), req.Question)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not answer: "+err.Error())
	}
	_, _ = h.credits.Charge(studentID, service.ActionDoubt)
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
