package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/wa"
	"github.com/gofiber/fiber/v2"
	"github.com/xuri/excelize/v2"
)

// maxCampaignRecipients caps a single broadcast to keep memory + Meta rate use
// sane. Larger lists should be split.
const maxCampaignRecipients = 50000

// WaCampaignHandler powers WhatsApp broadcast campaigns: parse an uploaded
// Excel, create a campaign (upload the header image once, enqueue one job per
// recipient on RabbitMQ), report progress, and retry failures.
type WaCampaignHandler struct {
	wa        *wa.Provider
	pub       *wa.Publisher
	repo      *repository.WaCampaignRepository
	templates *repository.WaTemplateRepository
}

// NewWaCampaignHandler builds a WaCampaignHandler.
func NewWaCampaignHandler(w *wa.Provider, pub *wa.Publisher,
	repo *repository.WaCampaignRepository,
	templates *repository.WaTemplateRepository) *WaCampaignHandler {
	return &WaCampaignHandler{wa: w, pub: pub, repo: repo, templates: templates}
}

// ParseExcel reads an uploaded .xlsx (base64) and returns its header row + data
// rows so the admin can preview and map columns to template variables.
// POST /api/v1/admin/wa/campaign/parse-excel  { "file_base64": "..." }
func (h *WaCampaignHandler) ParseExcel(c *fiber.Ctx) error {
	var req struct {
		FileBase64 string `json:"file_base64"`
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	data, err := decodeImage(req.FileBase64) // plain base64 (handles data-url too)
	if err != nil || len(data) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid or empty file")
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "could not read the Excel file: "+err.Error())
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "the workbook has no sheets")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not read rows")
	}
	if len(rows) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "the sheet is empty")
	}
	headers := rows[0]
	var dataRows [][]string
	if len(rows) > 1 {
		dataRows = rows[1:]
	}
	if len(dataRows) > maxCampaignRecipients {
		return fiber.NewError(fiber.StatusBadRequest,
			"too many rows (max "+strconv.Itoa(maxCampaignRecipients)+")")
	}
	return c.JSON(fiber.Map{
		"success": true, "sheet": sheets[0],
		"headers": headers, "rows": dataRows, "count": len(dataRows),
	})
}

// SampleExcel returns a ready-to-fill .xlsx (base64) with a "phone" column plus
// one "var1..varN" column per template variable and two example rows, so the
// admin knows the exact upload format.
// GET /api/v1/admin/wa/campaign/sample?vars=2
func (h *WaCampaignHandler) SampleExcel(c *fiber.Ctx) error {
	vars, _ := strconv.Atoi(c.Query("vars", "0"))
	if vars < 0 {
		vars = 0
	}
	if vars > 10 {
		vars = 10
	}
	headers := []string{"phone"}
	ex1 := []string{"919876543210"}
	ex2 := []string{"919123456780"}
	for i := 1; i <= vars; i++ {
		headers = append(headers, "var"+strconv.Itoa(i))
		ex1 = append(ex1, "Sample"+strconv.Itoa(i))
		ex2 = append(ex2, "Example"+strconv.Itoa(i))
	}
	f := excelize.NewFile()
	defer f.Close()
	sheet := f.GetSheetName(0)
	writeRow(f, sheet, 1, headers)
	writeRow(f, sheet, 2, ex1)
	writeRow(f, sheet, 3, ex2)
	buf, err := f.WriteToBuffer()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not build sample")
	}
	return c.JSON(fiber.Map{
		"success":  true,
		"filename": "sample_recipients.xlsx",
		"data":     base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
}

// writeRow writes string values across a single 1-indexed row.
func writeRow(f *excelize.File, sheet string, row int, vals []string) {
	for i, v := range vals {
		cell, _ := excelize.CoordinatesToCellName(i+1, row)
		_ = f.SetCellValue(sheet, cell, v)
	}
}

type createCampaignReq struct {
	Name         string `json:"name"`
	TemplateName string `json:"template_name"`
	TemplateLang string `json:"template_lang"`
	ImageBase64  string `json:"image_base64"` // optional header image
	ImageMime    string `json:"image_mime"`
	ScheduledAt  string `json:"scheduled_at"` // RFC3339; empty/past = send now
	Recipients   []struct {
		Phone  string   `json:"phone"`
		Params []string `json:"params"`
	} `json:"recipients"`
}

// Create builds a campaign and enqueues one WhatsApp job per recipient. The
// header image (if any) is uploaded to Meta once and reused for every send.
// POST /api/v1/admin/wa/campaign
func (h *WaCampaignHandler) Create(c *fiber.Ctx) error {
	if h.wa == nil || h.pub == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "WhatsApp not wired")
	}
	var req createCampaignReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.TemplateName = strings.TrimSpace(req.TemplateName)
	if req.TemplateName == "" {
		return fiber.NewError(fiber.StatusBadRequest, "template is required")
	}
	if !h.pub.Enabled() {
		return fiber.NewError(fiber.StatusBadRequest,
			"WhatsApp is not configured — set the token + phone number id in Settings")
	}
	// Collect valid recipients (must have a phone).
	type vr struct {
		phone  string
		params []string
	}
	valid := make([]vr, 0, len(req.Recipients))
	for _, r := range req.Recipients {
		p := strings.TrimSpace(r.Phone)
		if p == "" {
			continue
		}
		valid = append(valid, vr{phone: p, params: r.Params})
	}
	if len(valid) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "no valid recipients (each needs a phone)")
	}
	if len(valid) > maxCampaignRecipients {
		return fiber.NewError(fiber.StatusBadRequest,
			"too many recipients (max "+strconv.Itoa(maxCampaignRecipients)+")")
	}
	lang := strings.TrimSpace(req.TemplateLang)
	if lang == "" {
		lang = "en_US"
	}

	// Optional schedule: if set and in the future, defer the send.
	var schedAt *time.Time
	if s := strings.TrimSpace(req.ScheduledAt); s != "" {
		if t, perr := time.Parse(time.RFC3339, s); perr == nil {
			schedAt = &t
		}
	}
	scheduled := schedAt != nil && schedAt.After(time.Now())

	// Resolve the header image bytes (uploaded, else the template's stored image).
	img, _ := decodeImage(req.ImageBase64)
	if len(img) == 0 && h.templates != nil {
		if tpl, terr := h.templates.GetByNameLang(req.TemplateName, lang); terr == nil &&
			tpl.ImageData != "" {
			if b, derr := base64.StdEncoding.DecodeString(tpl.ImageData); derr == nil {
				img = b
			}
		}
	}
	if len(img) > 5*1024*1024 {
		return fiber.NewError(fiber.StatusBadRequest,
			"header image is too large — WhatsApp allows JPEG/PNG up to 5 MB (use a smaller image, ~1080px)")
	}

	// Immediate sends upload the image now; scheduled sends store the bytes and
	// upload a fresh media id at dispatch time (media ids expire ~30 days).
	mediaID := ""
	imgStore := ""
	if scheduled {
		if len(img) > 0 {
			imgStore = base64.StdEncoding.EncodeToString(img)
		}
	} else if len(img) > 0 {
		mime := req.ImageMime
		if mime == "" {
			mime = "image/jpeg"
		}
		id, err := h.wa.UploadMedia(c.Context(), img, "campaign", mime)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "header image upload failed: "+err.Error())
		}
		mediaID = id
	}

	status := "sending"
	if scheduled {
		status = "scheduled"
	}
	adminID, _ := c.Locals("admin_id").(uint)
	camp := &model.WaCampaign{
		Name: strings.TrimSpace(req.Name), TemplateName: req.TemplateName,
		TemplateLang: lang, HeaderImageID: mediaID, ImageData: imgStore,
		Status: status, ScheduledAt: schedAt, Total: len(valid), CreatedBy: adminID,
	}
	if err := h.repo.Create(camp); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not create the campaign")
	}

	recs := make([]*model.WaCampaignRecipient, len(valid))
	for i, v := range valid {
		pj, _ := json.Marshal(v.params)
		recs[i] = &model.WaCampaignRecipient{
			CampaignID: camp.ID, Phone: v.phone, Params: string(pj), Status: "queued",
		}
	}
	if err := h.repo.AddRecipients(recs); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not save recipients")
	}

	// Scheduled campaigns are enqueued later by the dispatcher cron.
	if scheduled {
		return c.JSON(fiber.Map{
			"success": true, "campaign_id": camp.ID, "total": len(valid), "scheduled": true,
			"message": "Campaign scheduled for " + schedAt.Local().Format("02 Jan 2006 3:04 PM") + ".",
		})
	}

	queued := 0
	for i, rec := range recs {
		if err := h.pub.Enqueue(wa.Job{
			Kind: "template", Phone: rec.Phone, Template: req.TemplateName, Lang: lang,
			Params: valid[i].params, HeaderImageID: mediaID,
			CampaignID: camp.ID, RecipientID: rec.ID,
		}); err != nil {
			continue
		}
		queued++
	}
	return c.JSON(fiber.Map{
		"success": true, "campaign_id": camp.ID, "total": len(valid), "queued": queued,
		"message": "Campaign queued — delivering in the background.",
	})
}

// List returns recent campaigns.
// GET /api/v1/admin/wa/campaign
func (h *WaCampaignHandler) List(c *fiber.Ctx) error {
	camps, err := h.repo.List(200)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load campaigns")
	}
	return c.JSON(fiber.Map{"success": true, "campaigns": camps})
}

// Get returns a campaign's live progress + its recipients (the report).
// GET /api/v1/admin/wa/campaign/:id
func (h *WaCampaignHandler) Get(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	camp, err := h.repo.Get(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "campaign not found")
	}
	recs, _ := h.repo.Recipients(uint(id), 2000)
	return c.JSON(fiber.Map{"success": true, "campaign": camp, "recipients": recs})
}

// Rename changes a campaign's display name (the only "edit" that makes sense
// once a broadcast has been sent — template/recipients can't change at Meta).
// PUT /api/v1/admin/wa/campaign/:id  { "name": "..." }
func (h *WaCampaignHandler) Rename(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.repo.Rename(uint(id), strings.TrimSpace(req.Name)); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not rename")
	}
	return c.JSON(fiber.Map{"success": true, "message": "Campaign renamed."})
}

// Delete removes a campaign and its recipients.
// DELETE /api/v1/admin/wa/campaign/:id
func (h *WaCampaignHandler) Delete(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	if err := h.repo.Delete(uint(id)); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not delete")
	}
	return c.JSON(fiber.Map{"success": true, "message": "Campaign deleted."})
}

// Resend re-sends the WHOLE campaign to every recipient again (unlike Retry,
// which only re-sends failures). Resets counters, then re-enqueues each row.
// POST /api/v1/admin/wa/campaign/:id/resend
func (h *WaCampaignHandler) Resend(c *fiber.Ctx) error {
	if h.pub == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "WhatsApp not wired")
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	camp, err := h.repo.Get(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "campaign not found")
	}
	if !h.pub.Enabled() {
		return fiber.NewError(fiber.StatusBadRequest, "WhatsApp is not configured")
	}
	recs, err := h.repo.AllRecipients(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load recipients")
	}
	if len(recs) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "this campaign has no recipients")
	}
	if err := h.repo.ResetForResend(uint(id)); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not reset the campaign")
	}
	requeued := 0
	for _, rec := range recs {
		var params []string
		_ = json.Unmarshal([]byte(rec.Params), &params)
		if err := h.pub.Enqueue(wa.Job{
			Kind: "template", Phone: rec.Phone, Template: camp.TemplateName,
			Lang: camp.TemplateLang, Params: params, HeaderImageID: camp.HeaderImageID,
			CampaignID: camp.ID, RecipientID: rec.ID,
		}); err != nil {
			continue
		}
		requeued++
	}
	return c.JSON(fiber.Map{"success": true, "requeued": requeued,
		"message": "Resending to " + strconv.Itoa(requeued) + " recipient(s)."})
}

// Retry re-queues a campaign's failed recipients.
// POST /api/v1/admin/wa/campaign/:id/retry
func (h *WaCampaignHandler) Retry(c *fiber.Ctx) error {
	if h.pub == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "WhatsApp not wired")
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	camp, err := h.repo.Get(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "campaign not found")
	}
	failed, err := h.repo.FailedRecipients(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load failed recipients")
	}
	if len(failed) == 0 {
		return c.JSON(fiber.Map{"success": true, "requeued": 0, "message": "No failed recipients to retry."})
	}
	ids := make([]uint, len(failed))
	for i, r := range failed {
		ids[i] = r.ID
	}
	if err := h.repo.RequeueFailed(uint(id), ids); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not requeue")
	}
	requeued := 0
	for _, rec := range failed {
		var params []string
		_ = json.Unmarshal([]byte(rec.Params), &params)
		if err := h.pub.Enqueue(wa.Job{
			Kind: "template", Phone: rec.Phone, Template: camp.TemplateName,
			Lang: camp.TemplateLang, Params: params, HeaderImageID: camp.HeaderImageID,
			CampaignID: camp.ID, RecipientID: rec.ID,
		}); err != nil {
			continue
		}
		requeued++
	}
	return c.JSON(fiber.Map{"success": true, "requeued": requeued,
		"message": "Re-queued " + strconv.Itoa(requeued) + " failed recipient(s)."})
}
