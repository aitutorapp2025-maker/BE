package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/wa"
	"github.com/gofiber/fiber/v2"
)

// WaInboxHandler powers the admin WhatsApp chat: the Meta webhook that
// receives customer messages, and the admin endpoints that list threads and
// send replies / promotional messages from the brand number. Both directions
// flow through RabbitMQ — the webhook enqueues raw payloads (wa.inbox) and
// sends enqueue delivery jobs (wa.send); workers do the real work.
type WaInboxHandler struct {
	messages *repository.WaMessageRepository
	settings *repository.SettingRepository
	pub      *wa.Publisher
	mq       *queue.RabbitMQ
}

// NewWaInboxHandler builds a WaInboxHandler.
func NewWaInboxHandler(messages *repository.WaMessageRepository,
	settings *repository.SettingRepository, pub *wa.Publisher,
	mq *queue.RabbitMQ) *WaInboxHandler {
	return &WaInboxHandler{messages: messages, settings: settings, pub: pub, mq: mq}
}

// verifyToken is the value the admin also types into Meta's webhook setup.
func (h *WaInboxHandler) verifyToken() string {
	if s, err := h.settings.Get(); err == nil &&
		strings.TrimSpace(s.WhatsappWebhookToken) != "" {
		return strings.TrimSpace(s.WhatsappWebhookToken)
	}
	return "vaha-wa-webhook"
}

// Verify answers Meta's webhook verification handshake.
// GET /api/v1/wa/webhook?hub.mode=subscribe&hub.verify_token=…&hub.challenge=…
func (h *WaInboxHandler) Verify(c *fiber.Ctx) error {
	if c.Query("hub.mode") == "subscribe" &&
		c.Query("hub.verify_token") == h.verifyToken() {
		return c.SendString(c.Query("hub.challenge"))
	}
	return fiber.NewError(fiber.StatusForbidden, "verification failed")
}

// Receive enqueues the raw Meta webhook payload to RabbitMQ and returns 200
// immediately (Meta retries on anything else); the inbox worker parses and
// stores the messages in the background.
// POST /api/v1/wa/webhook
func (h *WaInboxHandler) Receive(c *fiber.Ctx) error {
	body := make([]byte, len(c.Body()))
	copy(body, c.Body())
	if len(body) > 0 && h.mq != nil {
		_ = h.mq.Publish(wa.QueueWaInbox, body)
	}
	return c.JSON(fiber.Map{"success": true})
}

// Conversations lists chat threads for the inbox.
// GET /api/v1/admin/wa/conversations
func (h *WaInboxHandler) Conversations(c *fiber.Ctx) error {
	convs, err := h.messages.Conversations(200)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load conversations")
	}
	return c.JSON(fiber.Map{"success": true, "conversations": convs})
}

// Thread returns one phone's full chat (both directions, oldest first).
// GET /api/v1/admin/wa/thread/:phone
func (h *WaInboxHandler) Thread(c *fiber.Ctx) error {
	phone := strings.TrimSpace(c.Params("phone"))
	if phone == "" {
		return fiber.NewError(fiber.StatusBadRequest, "phone is required")
	}
	msgs, err := h.messages.Thread(phone, 500)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load the thread")
	}
	return c.JSON(fiber.Map{"success": true, "messages": msgs})
}

type waSendRequest struct {
	Phone string `json:"phone"`
	Text  string `json:"text"`
}

// Send queues a reply / promotional message on RabbitMQ; the WhatsApp worker
// delivers it from the brand number and records it in the thread.
// POST /api/v1/admin/wa/send { "phone": "…", "text": "…" }
func (h *WaInboxHandler) Send(c *fiber.Ctx) error {
	var req waSendRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Phone = strings.TrimSpace(req.Phone)
	req.Text = strings.TrimSpace(req.Text)
	if req.Phone == "" || req.Text == "" {
		return fiber.NewError(fiber.StatusBadRequest, "phone and text are required")
	}
	if h.pub == nil || !h.pub.Enabled() {
		return fiber.NewError(fiber.StatusBadRequest,
			"WhatsApp is not configured — set the token + phone number id in Settings")
	}
	if err := h.pub.Enqueue(wa.Job{
		Phone: req.Phone, Text: req.Text, Kind: "chat",
	}); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not queue the message")
	}
	return c.JSON(fiber.Map{
		"success": true,
		"message": "Queued — delivering via WhatsApp now.",
	})
}

