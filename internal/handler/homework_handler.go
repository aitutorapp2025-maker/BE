package handler

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/cryptox"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

// sseStream sets Server-Sent Events headers and runs fn as the body writer.
// writeEvent marshals v to JSON and, when the request carried an E2E session key
// (set by middleware.DecryptRequest), encrypts each frame's payload — so the
// whole stream stays end-to-end encrypted, frame by frame — before writing it as
// `data: <payload>\n\n`. Shared by the streaming Ask/Teach/Doubt/Grade handlers.
// The fiber ctx must not be touched inside fn (it runs after the handler
// returns), so the key is captured up front here.
func sseStream(c *fiber.Ctx, fn func(writeEvent func(v any))) error {
	key, _ := c.Locals("enckey").([]byte)
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")
	if len(key) > 0 {
		c.Set("X-Encrypted", "1") // frames are AES-GCM envelopes
	}
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		fn(func(v any) {
			b, err := json.Marshal(v)
			if err != nil {
				return
			}
			payload := b
			if len(key) > 0 {
				enc, eerr := cryptox.Encrypt(key, b)
				if eerr != nil {
					return
				}
				payload = enc
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return
			}
			_ = w.Flush()
		})
	})
	return nil
}

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

type uploadAttachment struct {
	Data      string `json:"data"`       // base64 bytes (no data: prefix)
	MediaType string `json:"media_type"` // whitelisted below
}

type uploadHomeworkRequest struct {
	Image     string `json:"image"`      // legacy single file (older app builds)
	MediaType string `json:"media_type"` // image/jpeg | image/png | application/pdf
	// Attachments is the multi-file form: several homework pages/documents that
	// together are ONE homework (photos, PDFs, Word/PPT/text).
	Attachments []uploadAttachment `json:"attachments"`
	// Note is what the student typed or SPOKE (dictated on-device) about the
	// homework — the AI takes it into account when planning the tasks.
	Note string `json:"note"`
}

// allowedHomeworkTypes is the upload whitelist: photos, PDF, Word (.docx),
// PowerPoint (.pptx) and plain text.
var allowedHomeworkTypes = map[string]bool{
	"image/jpeg":      true,
	"image/jpg":       true,
	"image/png":       true,
	"application/pdf": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
	"text/plain": true,
}

const maxHomeworkFiles = 6

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
	// Collect files: the multi-file form, or the legacy single-image form.
	raws := req.Attachments
	if len(raws) == 0 && strings.TrimSpace(req.Image) != "" {
		raws = []uploadAttachment{{Data: req.Image, MediaType: req.MediaType}}
	}
	if len(raws) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "no files provided")
	}
	if len(raws) > maxHomeworkFiles {
		return fiber.NewError(fiber.StatusBadRequest,
			fmt.Sprintf("too many files — please upload at most %d per homework", maxHomeworkFiles))
	}
	atts := make([]service.HomeworkAttachment, 0, len(raws))
	total := 0
	for _, a := range raws {
		mt := strings.ToLower(strings.TrimSpace(a.MediaType))
		if !allowedHomeworkTypes[mt] {
			return fiber.NewError(fiber.StatusUnsupportedMediaType,
				"unsupported file type — allowed: JPG/PNG photos, PDF, Word (.docx), PowerPoint (.pptx) and text files")
		}
		// Tolerate a data URL prefix if the client sent one.
		data := a.Data
		if i := strings.Index(data, ","); strings.HasPrefix(data, "data:") && i > 0 {
			data = data[i+1:]
		}
		bytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(data))
		if err != nil || len(bytes) == 0 {
			return fiber.NewError(fiber.StatusBadRequest, "one of the files is not readable")
		}
		if len(bytes) > maxHomeworkBytes {
			return fiber.NewError(fiber.StatusRequestEntityTooLarge,
				"one file is too large — each file must be under 12 MB")
		}
		total += len(bytes)
		if total > 2*maxHomeworkBytes {
			return fiber.NewError(fiber.StatusRequestEntityTooLarge,
				"the files together are too large — please upload fewer or smaller files")
		}
		atts = append(atts, service.HomeworkAttachment{Data: bytes, MediaType: mt})
	}
	if err := h.rateLimitUpload(c.Context(), studentID); err != nil {
		return err
	}
	if err := h.requireCredits(studentID, service.ActionHomeworkRead); err != nil {
		return err
	}
	hw, err := h.hw.CreateFromAttachments(c.Context(), studentID, atts, req.Note)
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

// TeachStream is the streaming (SSE) variant of Teach: the lesson streams in
// token-by-token. Signed + end-to-end encrypted (see AskStream).
// POST /api/v1/student/homework/:id/tasks/:taskId/teach/stream
func (h *HomeworkHandler) TeachStream(c *fiber.Ctx) error {
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
	hwSvc, credits := h.hw, h.credits
	sid, hid, tid := studentID, uint(hwID), uint(taskID)
	return sseStream(c, func(writeEvent func(v any)) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		res, cached, err := hwSvc.TeachTaskStream(ctx, sid, hid, tid, func(d string) {
			writeEvent(map[string]any{"type": "delta", "text": d})
		})
		if err != nil {
			writeEvent(map[string]string{"type": "error", "message": "Sorry, the lesson could not be loaded. Please try again."})
			return
		}
		if !cached {
			_, _ = credits.Charge(sid, service.ActionTeach)
		}
		writeEvent(map[string]any{"type": "done", "grounded": res.Grounded})
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

// GradeTestStream grades typed answers while streaming the feedback summary
// token-by-token (SSE); the final `done` event carries the full graded test
// (score + per-question feedback). Signed + end-to-end encrypted (see AskStream).
// POST /api/v1/student/homework/:id/test/grade/stream
func (h *HomeworkHandler) GradeTestStream(c *fiber.Ctx) error {
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
	action := service.ActionWrittenExam
	if req.Kind == "oral" {
		action = service.ActionOralExam
	}
	if err := h.requireCredits(studentID, action); err != nil {
		return err
	}
	hwSvc, credits := h.hw, h.credits
	sid, hid := studentID, uint(hwID)
	questions, answers, kind := req.Questions, req.Answers, req.Kind
	return sseStream(c, func(writeEvent func(v any)) {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		test, err := hwSvc.GradeWrittenStream(ctx, sid, hid, questions, answers, kind, func(d string) {
			writeEvent(map[string]any{"type": "delta", "text": d})
		})
		if err != nil {
			writeEvent(map[string]string{"type": "error", "message": "Sorry, I couldn't grade that just now. Please try again."})
			return
		}
		_, _ = credits.Charge(sid, action)
		writeEvent(map[string]any{"type": "done", "test": test})
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

// DoubtImage answers a doubt sent as a PHOTO (book/notebook question, possibly
// handwritten) during an in-chat learning session, scoped to a task.
// POST /api/v1/student/homework/:id/tasks/:taskId/doubt-image
func (h *HomeworkHandler) DoubtImage(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	hwID, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	taskID, _ := strconv.ParseUint(c.Params("taskId"), 10, 64)
	if hwID == 0 || taskID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid ids")
	}
	var req struct {
		Image     string `json:"image"` // base64
		MediaType string `json:"media_type"`
		Question  string `json:"question"` // optional typed/spoken question
	}
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.Image) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "a photo is required")
	}
	mt := strings.ToLower(strings.TrimSpace(req.MediaType))
	if mt != "image/jpeg" && mt != "image/jpg" && mt != "image/png" {
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "please send a JPG or PNG photo")
	}
	if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.Image)); err != nil ||
		len(raw) == 0 || len(raw) > maxHomeworkBytes {
		return fiber.NewError(fiber.StatusBadRequest, "invalid or oversized photo")
	}
	if err := h.requireCredits(studentID, service.ActionDoubt); err != nil {
		return err
	}
	res, err := h.hw.AskDoubtImage(c.Context(), studentID, uint(hwID), uint(taskID),
		req.Question, strings.TrimSpace(req.Image), mt)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not answer: "+err.Error())
	}
	_, _ = h.credits.Charge(studentID, service.ActionDoubt)
	return c.JSON(fiber.Map{"success": true, "answer": res.Answer})
}

// DoubtStream is the streaming (SSE) variant of Doubt: the answer streams in
// token-by-token. Signed + end-to-end encrypted (see AskStream).
// POST /api/v1/student/homework/:id/tasks/:taskId/doubt/stream  { "question": "..." }
func (h *HomeworkHandler) DoubtStream(c *fiber.Ctx) error {
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
	hwSvc, credits := h.hw, h.credits
	sid, hid, tid := studentID, uint(hwID), uint(taskID)
	question := strings.TrimSpace(req.Question)
	return sseStream(c, func(writeEvent func(v any)) {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		res, err := hwSvc.AskDoubtStream(ctx, sid, hid, tid, question, func(d string) {
			writeEvent(map[string]any{"type": "delta", "text": d})
		})
		if err != nil {
			writeEvent(map[string]string{"type": "error", "message": "Sorry, I couldn't answer that just now. Please try again."})
			return
		}
		_, _ = credits.Charge(sid, service.ActionDoubt)
		writeEvent(map[string]any{"type": "done", "grounded": res.Grounded})
	})
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

// RescheduleTask moves one task's study time to the student's chosen time and
// re-arms its "time to study" push reminder.
// PUT /api/v1/student/homework/:id/tasks/:taskId/schedule
func (h *HomeworkHandler) RescheduleTask(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	taskID, _ := strconv.ParseUint(c.Params("taskId"), 10, 64)
	if taskID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid task id")
	}
	var req struct {
		ScheduledAt string `json:"scheduled_at"` // RFC3339
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ScheduledAt))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid scheduled_at (want RFC3339)")
	}
	hw, err := h.hw.RescheduleTask(studentID, uint(taskID), at)
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
