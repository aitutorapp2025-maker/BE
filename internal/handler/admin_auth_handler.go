package handler

import (
	"errors"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
)

// AdminAuthHandler handles admin authentication endpoints.
type AdminAuthHandler struct {
	auth   *service.AuthService
	admins *repository.AdminRepository
}

// NewAdminAuthHandler builds an AdminAuthHandler.
func NewAdminAuthHandler(auth *service.AuthService, admins *repository.AdminRepository) *AdminAuthHandler {
	return &AdminAuthHandler{auth: auth, admins: admins}
}

// loginRequest is the JSON body for POST /admin/login.
type loginRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	ClientPub string `json:"client_pub"` // base64 X25519 pubkey for E2E key exchange
}

// Login authenticates an admin and returns a JWT.
//
// POST /api/v1/admin/login  { "email": "...", "password": "..." }
func (h *AdminAuthHandler) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "email and password are required")
	}

	result, err := h.auth.AdminLogin(c.Context(), req.Email, req.Password, req.ClientPub)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid email or password")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "login failed")
	}

	return c.JSON(fiber.Map{
		"success":        true,
		"token":          result.Token,
		"token_type":     "Bearer",
		"refresh_token":  result.RefreshToken,
		"signing_secret": result.SigningSecret,
		"server_pub":     result.ServerPub,
		"expires_at":     result.ExpiresAt,
		"admin":          result.Admin,
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh rotates the refresh token and issues a new access token.
// POST /api/v1/admin/refresh  { "refresh_token": "..." }  (no signature required)
func (h *AdminAuthHandler) Refresh(c *fiber.Ctx) error {
	var req refreshRequest
	if err := c.BodyParser(&req); err != nil || strings.TrimSpace(req.RefreshToken) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "refresh_token is required")
	}
	result, err := h.auth.Refresh(c.Context(), req.RefreshToken)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "session expired — please sign in again")
	}
	return c.JSON(fiber.Map{
		"success":       true,
		"token":         result.Token,
		"token_type":    "Bearer",
		"refresh_token": result.RefreshToken,
		"expires_at":    result.ExpiresAt,
		"admin":         result.Admin,
	})
}

// Logout ends the current session.
// POST /api/v1/admin/logout
func (h *AdminAuthHandler) Logout(c *fiber.Ctx) error {
	sid, _ := c.Locals("sid").(string)
	if sid != "" {
		_ = h.auth.Logout(c.Context(), sid)
	}
	return c.JSON(fiber.Map{"success": true})
}

// Me returns the currently authenticated admin (requires a valid token).
//
// GET /api/v1/admin/me  (Authorization: Bearer <token>)
func (h *AdminAuthHandler) Me(c *fiber.Ctx) error {
	adminID, _ := c.Locals("admin_id").(uint)
	admin, err := h.admins.FindByID(adminID)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "account not found")
	}
	return c.JSON(fiber.Map{"success": true, "admin": admin})
}

type changePasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// ChangePassword updates the signed-in admin's password.
//
// POST /api/v1/admin/change-password  { "new_password": "..." }
func (h *AdminAuthHandler) ChangePassword(c *fiber.Ctx) error {
	adminID, _ := c.Locals("admin_id").(uint)

	var req changePasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if len(strings.TrimSpace(req.NewPassword)) < 4 {
		return fiber.NewError(fiber.StatusBadRequest, "password must be at least 4 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to hash password")
	}
	if err := h.admins.UpdatePassword(adminID, string(hash)); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update password")
	}
	return c.JSON(fiber.Map{"success": true})
}
