package handler

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/fcm"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// SupportHandler serves the "Report a problem" ticket endpoints — students file
// and track their own tickets; admins list and respond.
type SupportHandler struct {
	svc     *service.SupportService
	tickets *repository.SupportRepository
	push    *fcm.Publisher // notify the student when an admin responds
}

// NewSupportHandler builds a SupportHandler.
func NewSupportHandler(svc *service.SupportService, tickets *repository.SupportRepository, push *fcm.Publisher) *SupportHandler {
	return &SupportHandler{svc: svc, tickets: tickets, push: push}
}

type createTicketRequest struct {
	Message   string `json:"message"`
	Image     string `json:"image"`      // optional base64 attachment (image/PDF)
	MediaType string `json:"media_type"` // image/jpeg | image/png | application/pdf
}

// Create files a new support ticket for the signed-in student.
// POST /api/v1/student/support
func (h *SupportHandler) Create(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	var req createTicketRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	var attachment []byte
	if img := strings.TrimSpace(req.Image); img != "" {
		if i := strings.Index(img, ","); strings.HasPrefix(img, "data:") && i > 0 {
			img = img[i+1:]
		}
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(img))
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid attachment")
		}
		attachment = b
	}
	label, _ := c.Locals("phone").(string)
	t, err := h.svc.Create(studentID, label, req.Message, attachment, req.MediaType)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "ticket": t})
}

// ListMine returns the student's own tickets (their issue history).
// GET /api/v1/student/support
func (h *SupportHandler) ListMine(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	list, err := h.tickets.ListByStudent(studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load your reports")
	}
	return c.JSON(fiber.Map{"success": true, "tickets": list})
}

// --- admin ---------------------------------------------------------------

// AdminList returns all tickets (optionally ?status=open|in_progress|resolved).
// GET /api/v1/admin/support
func (h *SupportHandler) AdminList(c *fiber.Ctx) error {
	list, err := h.tickets.List(strings.TrimSpace(c.Query("status")), 300)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load tickets")
	}
	open, _ := h.tickets.CountOpen()
	return c.JSON(fiber.Map{"success": true, "tickets": list, "open_count": open})
}

type replyTicketRequest struct {
	Reply  string `json:"reply"`
	Status string `json:"status"` // open | in_progress | resolved
}

// AdminReply sets the admin response + status on a ticket.
// PUT /api/v1/admin/support/:id
func (h *SupportHandler) AdminReply(c *fiber.Ctx) error {
	id, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	if id == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid ticket id")
	}
	var req replyTicketRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	t, err := h.svc.Reply(uint(id), req.Reply, req.Status)
	if err != nil {
		return notFoundOrInternal(err, "ticket")
	}
	// Notify the student their report was answered/updated — enqueued to
	// RabbitMQ so the admin request returns immediately and delivery (FCM push +
	// in-app notification) happens in the background push worker. Best-effort.
	h.notifyStudent(t, req.Reply)
	return c.JSON(fiber.Map{"success": true, "ticket": t})
}

// notifyStudent enqueues a push telling the student about the admin response.
// newReply is the reply text submitted in THIS update (empty = status-only),
// so a status change doesn't re-announce an earlier reply.
func (h *SupportHandler) notifyStudent(t *model.SupportTicket, newReply string) {
	if h.push == nil || !h.push.Enabled() || t == nil || t.StudentID == 0 {
		return
	}
	body := strings.TrimSpace(newReply)
	if body == "" {
		body = "Your report status is now " + statusLabel(t.Status) + "."
	}
	_ = h.push.Enqueue(fcm.PushJob{
		Title:      "Support update",
		Body:       trunc(body, 160),
		StudentIDs: []uint{t.StudentID},
	})
}

func statusLabel(s string) string {
	switch s {
	case "in_progress":
		return "In progress"
	case "resolved":
		return "Resolved"
	default:
		return "Open"
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}
