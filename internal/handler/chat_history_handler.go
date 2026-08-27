package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// ChatHistoryHandler stores + serves the signed-in student's AI-tutor chat so
// it syncs across devices and survives a reinstall. It also receives voice
// notes: the audio file is stored (played back WhatsApp-style in the chat)
// and transcribed via Gemini.
type ChatHistoryHandler struct {
	chats      *repository.ChatMessageRepository
	ai         *ai.Chat
	uploadsDir string
	publicBase string
}

// NewChatHistoryHandler builds a ChatHistoryHandler. aiChat powers voice-note
// transcription; uploadsDir/publicBase place + address the stored audio.
func NewChatHistoryHandler(chats *repository.ChatMessageRepository, aiChat *ai.Chat, uploadsDir, publicBase string) *ChatHistoryHandler {
	return &ChatHistoryHandler{
		chats: chats, ai: aiChat, uploadsDir: uploadsDir, publicBase: publicBase,
	}
}

// allowedVoiceTypes is the voice-note upload whitelist.
var allowedVoiceTypes = map[string]string{
	"audio/mp4":  ".m4a",
	"audio/aac":  ".m4a",
	"audio/mpeg": ".mp3",
	"audio/ogg":  ".ogg",
	"audio/wav":  ".wav",
}

// maxVoiceBytes caps a decoded voice note (~2 min of AAC is well under this).
const maxVoiceBytes = 8 << 20 // 8 MB

// Voice accepts a recorded voice note: stores the audio (so the student can
// replay it in the chat, on any device) and transcribes it in the spoken
// language. POST /api/v1/student/chat/voice  (signed + encrypted)
func (h *ChatHistoryHandler) Voice(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	var req struct {
		Audio     string `json:"audio"`      // base64
		MediaType string `json:"media_type"` // audio/mp4 etc.
		Language  string `json:"language"`   // hint, e.g. "tamil"
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	mt := strings.ToLower(strings.TrimSpace(req.MediaType))
	ext, ok := allowedVoiceTypes[mt]
	if !ok {
		return fiber.NewError(fiber.StatusUnsupportedMediaType, "unsupported audio type")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.Audio))
	if err != nil || len(raw) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid audio data")
	}
	if len(raw) > maxVoiceBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge,
			"that recording is too long — please keep it under 2 minutes")
	}

	// Store under an unguessable random name (capability URL).
	rnd := make([]byte, 16)
	if _, err := rand.Read(rnd); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to store the recording")
	}
	dir := filepath.Join(h.uploadsDir, "voice")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to store the recording")
	}
	name := "v" + hex.EncodeToString(rnd) + ext
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to store the recording")
	}
	base := strings.TrimRight(h.publicBase, "/")
	if base == "" {
		base = strings.TrimRight(c.BaseURL(), "/")
	}
	url := base + "/uploads/voice/" + name

	// Transcribe in the spoken language (Gemini audio understanding).
	transcript, terr := h.ai.TranscribeAudio(c.Context(),
		base64.StdEncoding.EncodeToString(raw), mt, strings.TrimSpace(req.Language))
	if terr != nil {
		return fiber.NewError(fiber.StatusBadGateway,
			"could not understand the recording: "+terr.Error())
	}
	if strings.TrimSpace(transcript) == "" {
		return fiber.NewError(fiber.StatusBadGateway,
			"could not hear anything in the recording — please try again")
	}
	return c.JSON(fiber.Map{"success": true, "url": url, "transcript": transcript})
}

// List returns the student's conversation, oldest first. The optional ?conv=
// query scopes to one chat thread ("" keeps the old full-history behavior for
// older app builds).
// GET /api/v1/student/chat  (signed + encrypted)
func (h *ChatHistoryHandler) List(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	items, err := h.chats.ListByStudent(studentID, strings.TrimSpace(c.Query("conv")))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load chat")
	}
	return c.JSON(fiber.Map{"success": true, "messages": items})
}

type chatSyncItem struct {
	ClientID   string `json:"client_id"`
	ConvID     string `json:"conv_id"`
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
		case "image", "pdf", "voice":
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
			ConvID:     strings.TrimSpace(m.ConvID),
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

	items, err := h.chats.ListByStudent(studentID, strings.TrimSpace(c.Query("conv")))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load chat")
	}
	return c.JSON(fiber.Map{"success": true, "messages": items})
}

// Conversations returns the student's chat threads, most recent first.
// GET /api/v1/student/chat/conversations  (signed + encrypted)
func (h *ChatHistoryHandler) Conversations(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	items, err := h.chats.ListConversations(studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load chats")
	}
	return c.JSON(fiber.Map{"success": true, "conversations": items})
}

type convSyncItem struct {
	ConvID string `json:"conv_id"`
	Name   string `json:"name"`
	Named  bool   `json:"named"`   // student renamed it (custom name wins)
	LastAt int64  `json:"last_at"` // ms since epoch
}

// SyncConversations upserts the app's conversation registry and returns the
// merged list, so threads (and their names) follow the student across devices.
// POST /api/v1/student/chat/conversations/sync  (signed + encrypted)
func (h *ChatHistoryHandler) SyncConversations(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	var req struct {
		Conversations []convSyncItem `json:"conversations"`
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	rows := make([]model.ChatConversation, 0, len(req.Conversations))
	for _, cv := range req.Conversations {
		id := strings.TrimSpace(cv.ConvID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(cv.Name)
		if len(name) > 60 {
			name = name[:60]
		}
		lastAt := time.Now()
		if cv.LastAt > 0 {
			lastAt = time.UnixMilli(cv.LastAt)
		}
		rows = append(rows, model.ChatConversation{
			ConvID: id, Name: name, Named: cv.Named, LastAt: lastAt,
		})
	}
	if err := h.chats.UpsertConversations(studentID, rows); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not save chats")
	}
	items, err := h.chats.ListConversations(studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load chats")
	}
	return c.JSON(fiber.Map{"success": true, "conversations": items})
}

// DeleteConversation removes one chat thread and all of its messages.
// DELETE /api/v1/student/chat/conversations/:convId  (signed + encrypted)
func (h *ChatHistoryHandler) DeleteConversation(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	convID := strings.TrimSpace(c.Params("convId"))
	if convID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "invalid conversation id")
	}
	if err := h.chats.DeleteConversation(studentID, convID); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not delete the chat")
	}
	return c.JSON(fiber.Map{"success": true})
}
