package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/auth"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/session"
	"github.com/gofiber/fiber/v2"
)

const (
	// clockSkew is how far the client clock may differ from the server.
	clockSkew = 90 * time.Second
	// nonceTTL keeps a used nonce long enough to cover the whole valid window.
	nonceTTL = 3 * time.Minute
)

// SignedAdmin authenticates a request with:
//  1. a valid short-lived access JWT (identity + session id),
//  2. a timestamp within the allowed clock skew,
//  3. an HMAC signature over METHOD\nURL\nsha256(body)\nnonce\ntimestamp using
//     the session's signing secret, and
//  4. a one-time nonce (Redis) — a repeated nonce is a replay and is rejected.
//
// Together these make every request carry a unique, single-use credential.
func SignedAdmin(cfg config.Config, store *session.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		claims, err := auth.ParseToken(cfg.JWT.Secret,
			strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil || claims.SID == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid or expired token")
		}

		nonce := c.Get("X-Nonce")
		tsStr := c.Get("X-Timestamp")
		sig := c.Get("X-Signature")
		if nonce == "" || tsStr == "" || sig == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "missing request signature")
		}

		tsMs, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid timestamp")
		}
		if diff := time.Since(time.UnixMilli(tsMs)); diff > clockSkew || diff < -clockSkew {
			return fiber.NewError(fiber.StatusUnauthorized, "request timestamp out of range")
		}

		secret, err := store.Secret(c.Context(), claims.SID)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "session expired — please sign in again")
		}

		bodyHash := sha256.Sum256(c.Body())
		signingString := strings.Join([]string{
			c.Method(),
			c.OriginalURL(),
			hex.EncodeToString(bodyHash[:]),
			nonce,
			tsStr,
		}, "\n")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(signingString))
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(sig)) {
			return fiber.NewError(fiber.StatusUnauthorized, "bad request signature")
		}

		fresh, err := store.UseNonce(c.Context(), claims.SID, nonce, nonceTTL)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "auth check failed")
		}
		if !fresh {
			return fiber.NewError(fiber.StatusUnauthorized, "request already used (replay)")
		}

		c.Locals("admin_id", claims.AdminID)
		c.Locals("email", claims.Email)
		c.Locals("role", claims.Role)
		c.Locals("sid", claims.SID)
		return c.Next()
	}
}
