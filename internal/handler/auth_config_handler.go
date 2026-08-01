package handler

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// AuthConfigHandler exposes the public login-screen config (which SSO methods
// are enabled + their public client ids), so the app can show/hide buttons.
type AuthConfigHandler struct {
	google service.GoogleConfigFunc
}

// NewAuthConfigHandler builds an AuthConfigHandler.
func NewAuthConfigHandler(google service.GoogleConfigFunc) *AuthConfigHandler {
	return &AuthConfigHandler{google: google}
}

// Get returns the enabled sign-in methods. GET /api/v1/auth-config (no auth).
func (h *AuthConfigHandler) Get(c *fiber.Ctx) error {
	gc := h.google()
	googleOn := gc.Enabled && gc.ClientID != ""
	clientID := ""
	if googleOn {
		clientID = gc.ClientID
	}
	return c.JSON(fiber.Map{
		"success":            true,
		"google_sso_enabled": googleOn,
		"google_client_id":   clientID,
	})
}
