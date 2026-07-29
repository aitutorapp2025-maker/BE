package handler

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// maxOgImageBytes caps the decoded OG image at 3 MB — plenty for a 1200×630
// share image and small enough to accept as base64 JSON on the admin path.
const maxOgImageBytes = 3 << 20

// ogImageExt maps an allowed image content-type to its file extension.
var ogImageExt = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// LandingUploadHandler stores admin-uploaded landing assets (the OG image) on
// disk under <dir>/og and returns their public URL. Files are served by the
// Fiber Static route mounted at /uploads.
type LandingUploadHandler struct {
	dir           string
	publicBaseURL string
}

// NewLandingUploadHandler builds a LandingUploadHandler.
func NewLandingUploadHandler(dir, publicBaseURL string) *LandingUploadHandler {
	return &LandingUploadHandler{dir: dir, publicBaseURL: publicBaseURL}
}

type ogImageUploadReq struct {
	Filename string `json:"filename"`
	Data     string `json:"data"` // base64 (raw or data: URL)
}

// UploadOgImage accepts a base64-encoded image and writes it to disk, returning
// its public URL. POST /api/v1/admin/landing/seo/og-image
func (h *LandingUploadHandler) UploadOgImage(c *fiber.Ctx) error {
	var req ogImageUploadReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	// Strip an optional data-URL prefix ("data:image/png;base64,").
	data := req.Data
	if i := strings.Index(data, ","); strings.HasPrefix(data, "data:") && i != -1 {
		data = data[i+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(data))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "image is not valid base64")
	}
	if len(raw) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "image is empty")
	}
	if len(raw) > maxOgImageBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "image exceeds 3 MB")
	}

	// Trust the bytes, not the client-supplied name, for the type.
	ct := http.DetectContentType(raw)
	ext, ok := ogImageExt[ct]
	if !ok {
		return fiber.NewError(fiber.StatusUnsupportedMediaType,
			"unsupported image type (use PNG, JPG, WEBP or GIF)")
	}

	dir := filepath.Join(h.dir, "og")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to store image")
	}
	name := fmt.Sprintf("og-%d%s", time.Now().UnixNano(), ext)
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to store image")
	}

	base := strings.TrimRight(h.publicBaseURL, "/")
	if base == "" {
		base = strings.TrimRight(c.BaseURL(), "/") // scheme://host from the request
	}
	url := base + "/uploads/og/" + name
	return c.JSON(fiber.Map{"success": true, "url": url})
}
