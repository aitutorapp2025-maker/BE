// Package middleware holds Fiber middleware (auth, etc.).
package middleware

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/auth"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/gofiber/fiber/v2"
)

// AdminAuth validates the Bearer JWT and stores the admin identity in locals.
// Handlers behind it can read c.Locals("admin_id").(uint), "email", "role".
func AdminAuth(cfg config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		tokenStr := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))

		claims, err := auth.ParseToken(cfg.JWT.Secret, tokenStr)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid or expired token")
		}

		c.Locals("admin_id", claims.AdminID)
		c.Locals("email", claims.Email)
		c.Locals("role", claims.Role)
		return c.Next()
	}
}
