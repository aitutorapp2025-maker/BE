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

// maxImageBytes caps the decoded image at 3 MB — plenty for a 1200×630 share
// image or a logo, and small enough to accept as base64 JSON on the admin path.
const maxImageBytes = 3 << 20

// imageExt maps an allowed image content-type to its file extension.
var imageExt = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// AssetUploadHandler stores admin-uploaded images (the landing OG image, the
// organisation logo) on disk under <dir>/<subdir> and returns their public URL.
// Files are served by the Fiber Static route mounted at /uploads.
type AssetUploadHandler struct {
	dir           string
	publicBaseURL string
}

// NewAssetUploadHandler builds an AssetUploadHandler.
func NewAssetUploadHandler(dir, publicBaseURL string) *AssetUploadHandler {
	return &AssetUploadHandler{dir: dir, publicBaseURL: publicBaseURL}
}

type imageUploadReq struct {
	Filename string `json:"filename"`
	Data     string `json:"data"` // base64 (raw or data: URL)
}

// UploadOgImage stores the landing OG image. POST /admin/landing/seo/og-image
func (h *AssetUploadHandler) UploadOgImage(c *fiber.Ctx) error {
	return h.save(c, "og")
}

// UploadLogo stores the organisation logo. POST /admin/settings/logo
func (h *AssetUploadHandler) UploadLogo(c *fiber.Ctx) error {
	return h.save(c, "logo")
}

// UploadNotificationImage stores an image for a push notification.
// POST /admin/notifications/image
func (h *AssetUploadHandler) UploadNotificationImage(c *fiber.Ctx) error {
	return h.save(c, "notif")
}

// save decodes a base64 image from the request, writes it under <dir>/<subdir>,
// and returns its public URL.
func (h *AssetUploadHandler) save(c *fiber.Ctx, subdir string) error {
	var req imageUploadReq
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
	if len(raw) > maxImageBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "image exceeds 3 MB")
	}

	// Trust the bytes, not the client-supplied name, for the type.
	ct := http.DetectContentType(raw)
	ext, ok := imageExt[ct]
	if !ok {
		return fiber.NewError(fiber.StatusUnsupportedMediaType,
			"unsupported image type (use PNG, JPG, WEBP or GIF)")
	}

	dir := filepath.Join(h.dir, subdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to store image")
	}
	name := fmt.Sprintf("%s-%d%s", subdir, time.Now().UnixNano(), ext)
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to store image")
	}

	base := strings.TrimRight(h.publicBaseURL, "/")
	if base == "" {
		base = strings.TrimRight(c.BaseURL(), "/") // scheme://host from the request
	}
	url := base + "/uploads/" + subdir + "/" + name
	return c.JSON(fiber.Map{"success": true, "url": url})
}
