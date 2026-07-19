package middleware

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/cryptox"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/session"
	"github.com/gofiber/fiber/v2"
)

// Encrypt provides end-to-end payload encryption for a session that completed
// the E2E key exchange. It decrypts an encrypted request body before the handler
// runs and encrypts a successful (2xx) response body afterwards. Requests/
// responses are opaque AES-256-GCM envelopes on the wire.
//
// Must run AFTER SignedAdmin (which sets "sid" and verifies the signature over
// the encrypted body). Error responses are left to the app ErrorHandler.
func Encrypt(store *session.Store) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Resolve the AES key from either the admin session (sid set by
		// SignedAdmin) or an anonymous session (X-Session header).
		var key []byte
		if sid, _ := c.Locals("sid").(string); sid != "" {
			key, _ = store.EncKey(c.Context(), sid)
		} else if anon := c.Get("X-Session"); anon != "" {
			key, _ = store.AnonEncKey(c.Context(), anon)
		}
		if len(key) == 0 {
			return c.Next() // no key — pass through in the clear
		}

		// Decrypt the request body.
		if c.Get("X-Encrypted") == "1" && len(c.Body()) > 0 {
			plain, derr := cryptox.Decrypt(key, c.Body())
			if derr != nil {
				return fiber.NewError(fiber.StatusBadRequest, "could not decrypt request")
			}
			c.Request().SetBody(plain)
		}

		if err := c.Next(); err != nil {
			return err // handled (plaintext) by the app ErrorHandler
		}

		// Encrypt the successful response body.
		body := c.Response().Body()
		if len(body) > 0 && c.Response().StatusCode() < 400 {
			enc, eerr := cryptox.Encrypt(key, body)
			if eerr == nil {
				c.Response().SetBody(enc)
				c.Response().Header.SetContentType(fiber.MIMEApplicationJSON)
				c.Set("X-Encrypted", "1")
			}
		}
		return nil
	}
}
