package handler

import (
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// ChatHistoryHandler stores + serves the signed-in student's AI-tutor chat so
// it syncs across devices and survives a reinstall.
type ChatHistoryHandler struct {
	chats *repository.ChatMessageRepository
}

// NewChatHistoryHandler builds a ChatHistoryHandler.
func NewChatHistoryHandler(chats *repository.ChatMessageRepository) *ChatHistoryHandler {
	return &ChatHistoryHandler{chats: chats}
}

// List returns the student's full conversation, oldest first.
// GET /api/v1/student/chat  (signed + encrypted)
func (h *ChatHistoryHandler) List(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	items, err := h.chats.ListByStudent(studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load chat")
	}
	return c.JSON(fiber.Map{"success": true, "messages": items})
}

type chatSyncItem struct {
	ClientID   string `json:"client_id"`
	Role       string `json:"role"`
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	HomeworkID uint   `json:"homework_id"`
	ImageURL   string `json:"image_url"`
	SentAt     int64  `json:"sent_at"` // ms since epoch
}

type chatSyncRequest struct {
	Messages []chatSyncItem `json:"messages"`
}

// Sync upserts the messages the app pushes (idempotent by client id) and returns
// the merged conversation, so a device can reconcile local + server history.
// POST /api/v1/student/chat/sync  (signed + encrypted)
func (h *ChatHistoryHandler) Sync(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	var req chatSyncRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	rows := make([]model.ChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		cid := strings.TrimSpace(m.ClientID)
		if cid == "" {
			continue
		}
		role := "ai"
		if strings.ToLower(m.Role) == "user" {
			role = "user"
		}
		kind := strings.ToLower(strings.TrimSpace(m.Kind))
		switch kind {
		case "image", "pdf":
			// keep
		default:
			kind = "text"
		}
		sentAt := time.Now()
		if m.SentAt > 0 {
			sentAt = time.UnixMilli(m.SentAt)
		}
		rows = append(rows, model.ChatMessage{
			StudentID:  studentID,
			ClientID:   cid,
			Role:       role,
			Kind:       kind,
			Text:       m.Text,
			HomeworkID: m.HomeworkID,
			ImageURL:   strings.TrimSpace(m.ImageURL),
			SentAt:     sentAt,
		})
	}

	if err := h.chats.UpsertBatch(rows); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not save chat")
	}

	items, err := h.chats.ListByStudent(studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load chat")
	}
	return c.JSON(fiber.Map{"success": true, "messages": items})
}
