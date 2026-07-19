package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/cryptox"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/session"
	"github.com/gofiber/fiber/v2"
)

// HandshakeHandler establishes an anonymous E2E session for unauthenticated
// clients (so public endpoints — landing, contact — can be encrypted too).
type HandshakeHandler struct {
	sessions *session.Store
}

// NewHandshakeHandler builds a HandshakeHandler.
func NewHandshakeHandler(sessions *session.Store) *HandshakeHandler {
	return &HandshakeHandler{sessions: sessions}
}

type handshakeRequest struct {
	ClientPub string `json:"client_pub"`
}

// Handshake performs the X25519 key exchange and returns an anonymous session id
// + the server public key. POST /api/v1/handshake  (public)
func (h *HandshakeHandler) Handshake(c *fiber.Ctx) error {
	var req handshakeRequest
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.ClientPub) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "client_pub is required")
	}
	key, serverPub, err := cryptox.ServerHandshake(req.ClientPub)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid handshake")
	}
	id := h.sessions.NewAnonID()
	if err := h.sessions.SetAnonEncKey(c.Context(), id, key); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "handshake failed")
	}
	return c.JSON(fiber.Map{
		"success":    true,
		"session_id": id,
		"server_pub": serverPub,
	})
}
