package middleware

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/auth"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/gofiber/fiber/v2"
)

// StudentAuth validates the Bearer student JWT and stores the student identity
// in locals. Handlers behind it can read c.Locals("student_id").(uint) and
// c.Locals("phone").(string).
func StudentAuth(cfg config.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		tokenStr := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))

		claims, err := auth.ParseStudentToken(cfg.JWT.Secret, tokenStr)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid or expired token")
		}

		c.Locals("student_id", claims.StudentID)
		c.Locals("phone", claims.Phone)
		return c.Next()
	}
}
