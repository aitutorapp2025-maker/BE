package handler

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/wa"
	"github.com/gofiber/fiber/v2"
)

// WaTemplateHandler manages WhatsApp message templates for broadcast campaigns:
// a local mirror (List) kept in sync with Meta (Sync), plus Create / Edit /
// Delete which call Meta and then update the local row.
type WaTemplateHandler struct {
	wa   *wa.Provider
	repo *repository.WaTemplateRepository
}

// NewWaTemplateHandler builds a WaTemplateHandler.
func NewWaTemplateHandler(w *wa.Provider, repo *repository.WaTemplateRepository) *WaTemplateHandler {
	return &WaTemplateHandler{wa: w, repo: repo}
}

// templateReq is the JSON body for create/edit. The header image travels as a
// base64 string (same pattern as the logo upload), so it flows through the
// signed/encrypted admin client. On edit, image_base64 is optional.
type templateReq struct {
	Name         string   `json:"name"`
	Language     string   `json:"language"`
	Category     string   `json:"category"`
	BodyText     string   `json:"body_text"`
	Footer       string   `json:"footer"`
	BodyExamples []string `json:"body_examples"`
	ImageBase64  string   `json:"image_base64"`
	ImageMime    string   `json:"image_mime"`
	Buttons      []struct {
		Type  string `json:"type"` // URL | PHONE_NUMBER | QUICK_REPLY
		Text  string `json:"text"`
		URL   string `json:"url"`
		Phone string `json:"phone_number"`
	} `json:"buttons"`
}

// toWaButtons converts request buttons to provider buttons, and also returns
// their JSON for the local mirror.
func toWaButtons(reqBtns []struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	URL   string `json:"url"`
	Phone string `json:"phone_number"`
}) ([]wa.TemplateButton, string) {
	btns := make([]wa.TemplateButton, 0, len(reqBtns))
	for _, b := range reqBtns {
		if strings.TrimSpace(b.Type) == "" || strings.TrimSpace(b.Text) == "" {
			continue
		}
		btns = append(btns, wa.TemplateButton{
			Type: b.Type, Text: b.Text, URL: b.URL, Phone: b.Phone,
		})
	}
	if len(btns) == 0 {
		return nil, ""
	}
	j, _ := json.Marshal(btns)
	return btns, string(j)
}

func decodeImage(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return nil, nil
	}
	// Tolerate a data-URL prefix ("data:image/png;base64,....").
	if i := strings.Index(b64, ","); strings.HasPrefix(b64, "data:") && i >= 0 {
		b64 = b64[i+1:]
	}
	return base64.StdEncoding.DecodeString(b64)
}

// List returns the locally-mirrored templates (fast; reflects the last sync).
// GET /api/v1/admin/wa/templates
func (h *WaTemplateHandler) List(c *fiber.Ctx) error {
	tpls, err := h.repo.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load templates")
	}
	for i := range tpls {
		tpls[i].HasImage = tpls[i].ImageData != ""
		tpls[i].ImageData = "" // never ship the raw image to the client
	}
	return c.JSON(fiber.Map{"success": true, "templates": tpls})
}

// Sync pulls the current templates + statuses from Meta and upserts them locally.
// POST /api/v1/admin/wa/templates/sync
func (h *WaTemplateHandler) Sync(c *fiber.Ctx) error {
	if h.wa == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "WhatsApp not wired")
	}
	remote, err := h.wa.ListTemplates(c.Context())
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	now := time.Now()
	synced := 0
	for _, t := range remote {
		btnsJSON := ""
		if len(t.Buttons) > 0 {
			if b, e := json.Marshal(t.Buttons); e == nil {
				btnsJSON = string(b)
			}
		}
		if err := h.repo.Upsert(&model.WaTemplate{
			MetaID: t.ID, Name: t.Name, Language: t.Language, Category: t.Category,
			Status: t.Status, HeaderFormat: t.HeaderFormat, BodyText: t.BodyText,
			BodyParams: t.BodyParams, RejectedReason: t.RejectedReason,
			Buttons: btnsJSON, LastSyncedAt: now,
		}); err != nil {
			continue
		}
		synced++
	}
	return c.JSON(fiber.Map{"success": true, "synced": synced,
		"message": "Synced " + strconv.Itoa(synced) + " template(s) from Meta."})
}

// Create submits a new image-header template to Meta and mirrors it as PENDING.
// POST /api/v1/admin/wa/templates
func (h *WaTemplateHandler) Create(c *fiber.Ctx) error {
	if h.wa == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "WhatsApp not wired")
	}
	var req templateReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "template name is required")
	}
	if strings.TrimSpace(req.BodyText) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "body text is required")
	}
	img, err := decodeImage(req.ImageBase64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid image data")
	}
	lang := strings.TrimSpace(req.Language)
	if lang == "" {
		lang = "en_US"
	}
	cat := strings.TrimSpace(req.Category)
	if cat == "" {
		cat = "MARKETING"
	}
	footer := strings.TrimSpace(req.Footer)

	btns, btnsJSON := toWaButtons(req.Buttons)
	id, status, err := h.wa.CreateTemplate(c.Context(), wa.CreateTemplateInput{
		Name: req.Name, Language: lang, Category: cat, BodyText: req.BodyText,
		BodyExamples: req.BodyExamples, Footer: footer,
		HeaderImage: img, HeaderMime: req.ImageMime, Buttons: btns,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	if status == "" {
		status = "PENDING"
	}
	headerFmt := ""
	if len(img) > 0 {
		headerFmt = "IMAGE"
	}
	_ = h.repo.Upsert(&model.WaTemplate{
		MetaID: id, Name: req.Name, Language: lang, Category: cat, Status: status,
		HeaderFormat: headerFmt, BodyText: req.BodyText, Footer: footer,
		BodyParams: len(req.BodyExamples), Buttons: btnsJSON, LastSyncedAt: time.Now(),
	})
	// Keep the header image so campaigns can auto-reuse it (no re-upload needed).
	if len(img) > 0 {
		_ = h.repo.SetImageData(req.Name, lang, base64.StdEncoding.EncodeToString(img))
	}
	return c.JSON(fiber.Map{"success": true, "id": id, "status": status,
		"message": "Template submitted — PENDING until Meta approves it."})
}

// Edit updates an existing template (by local id) at Meta, then mirrors it as
// PENDING. image_base64 is optional (omit to keep the current header).
// PUT /api/v1/admin/wa/templates/:id
func (h *WaTemplateHandler) Edit(c *fiber.Ctx) error {
	if h.wa == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "WhatsApp not wired")
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	tpl, err := h.repo.Get(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "template not found")
	}
	if strings.TrimSpace(tpl.MetaID) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "this template has no Meta id yet — Sync first")
	}
	var req templateReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	img, _ := decodeImage(req.ImageBase64) // optional on edit
	footer := strings.TrimSpace(req.Footer)
	cat := strings.TrimSpace(req.Category)

	btns, btnsJSON := toWaButtons(req.Buttons)
	if err := h.wa.EditTemplate(c.Context(), wa.EditTemplateInput{
		MetaID: tpl.MetaID, Category: cat, BodyText: req.BodyText,
		BodyExamples: req.BodyExamples, Footer: footer,
		HeaderImage: img, HeaderMime: req.ImageMime, Buttons: btns,
	}); err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	tpl.Buttons = btnsJSON
	if strings.TrimSpace(req.BodyText) != "" {
		tpl.BodyText = req.BodyText
		tpl.BodyParams = len(req.BodyExamples)
	}
	tpl.Footer = footer
	if cat != "" {
		tpl.Category = strings.ToUpper(cat)
	}
	tpl.Status = "PENDING"
	tpl.LastSyncedAt = time.Now()
	_ = h.repo.Upsert(tpl)
	// Update the stored header image only when a new one was provided.
	if len(img) > 0 {
		_ = h.repo.SetImageData(tpl.Name, tpl.Language, base64.StdEncoding.EncodeToString(img))
	}
	return c.JSON(fiber.Map{"success": true,
		"message": "Template updated — back to PENDING for Meta re-approval."})
}

// SetImage stores a header image LOCALLY for auto-reuse in campaigns, without
// changing the Meta template (so an APPROVED template stays approved — no
// re-approval). Use it to cache the image for an already-approved image-header
// template. POST /api/v1/admin/wa/templates/:id/image  { image_base64, image_mime }
func (h *WaTemplateHandler) SetImage(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	tpl, err := h.repo.Get(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "template not found")
	}
	var req struct {
		ImageBase64 string `json:"image_base64"`
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	img, derr := decodeImage(req.ImageBase64)
	if derr != nil || len(img) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "a valid image is required")
	}
	if len(img) > 5*1024*1024 {
		return fiber.NewError(fiber.StatusBadRequest,
			"image is too large — WhatsApp allows up to 5 MB (use a smaller image, ~1080px)")
	}
	if err := h.repo.SetImageData(tpl.Name, tpl.Language,
		base64.StdEncoding.EncodeToString(img)); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not save the image")
	}
	return c.JSON(fiber.Map{"success": true,
		"message": "Image saved — campaigns will reuse it automatically (no re-approval)."})
}

// Delete removes a template from Meta (by name) and locally.
// DELETE /api/v1/admin/wa/templates/:id
func (h *WaTemplateHandler) Delete(c *fiber.Ctx) error {
	if h.wa == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "WhatsApp not wired")
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	tpl, err := h.repo.Get(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "template not found")
	}
	if err := h.wa.DeleteTemplate(c.Context(), tpl.Name); err != nil {
		return fiber.NewError(fiber.StatusBadGateway, err.Error())
	}
	_ = h.repo.DeleteByName(tpl.Name)
	return c.JSON(fiber.Map{"success": true, "message": "Template deleted."})
}
