package handler

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// AuthConfigHandler exposes the public app config: the brand name + logo (so the
// app shows the admin-managed name everywhere) and which SSO methods are enabled
// (so it can show/hide login buttons).
type AuthConfigHandler struct {
	google   service.GoogleConfigFunc
	settings *repository.SettingRepository
}

// NewAuthConfigHandler builds an AuthConfigHandler.
func NewAuthConfigHandler(google service.GoogleConfigFunc, settings *repository.SettingRepository) *AuthConfigHandler {
	return &AuthConfigHandler{google: google, settings: settings}
}

// Get returns the public app config. GET /api/v1/auth-config (no auth).
func (h *AuthConfigHandler) Get(c *fiber.Ctx) error {
	gc := h.google()
	googleOn := gc.Enabled && gc.ClientID != ""
	clientID := ""
	if googleOn {
		clientID = gc.ClientID
	}
	// Brand name + logo + store links from admin Settings.
	appName := "Vaha AI"
	logoURL, androidURL, iosURL := "", "", ""
	castOn := false
	if s, err := h.settings.Get(); err == nil {
		if s.AppName != "" {
			appName = s.AppName
		}
		logoURL = s.LogoURL
		androidURL = s.AndroidStoreURL
		iosURL = s.IosStoreURL
		castOn = s.CastEnabled
	}
	return c.JSON(fiber.Map{
		"success":            true,
		"app_name":           appName,
		"logo_url":           logoURL,
		"android_store_url":  androidURL,
		"ios_store_url":      iosURL,
		"cast_enabled":       castOn,
		"google_sso_enabled": googleOn,
		"google_client_id":   clientID,
	})
}
