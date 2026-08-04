package handler

import (
	"path/filepath"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/media"
	"github.com/gofiber/fiber/v2"
)

// MediaHandler serves private uploads (homework photos) only when the request
// carries a valid signature — the files are random-named and never on the public
// /uploads mount, so they can't be enumerated or fetched without the token.
type MediaHandler struct {
	privateDir string
	secret     string
}

// NewMediaHandler builds a MediaHandler.
func NewMediaHandler(privateDir, secret string) *MediaHandler {
	return &MediaHandler{privateDir: privateDir, secret: secret}
}

// Homework serves a homework image if the signature matches.
// GET /api/v1/media/hw/:file?sig=...
func (h *MediaHandler) Homework(c *fiber.Ctx) error {
	file := c.Params("file")
	// Reject any path traversal — only a bare filename is allowed.
	if file == "" || strings.ContainsAny(file, "/\\") || strings.Contains(file, "..") {
		return fiber.NewError(fiber.StatusBadRequest, "bad file")
	}
	if !media.Verify(file, c.Query("sig"), h.secret) {
		return fiber.NewError(fiber.StatusForbidden, "invalid signature")
	}
	path := filepath.Join(h.privateDir, "homework", file)
	// SendFile 404s cleanly if the file is missing.
	return c.SendFile(path, true)
}
