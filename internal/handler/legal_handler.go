package handler

import (
	"errors"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// allowedLegalKeys are the legal documents that may be read/edited.
var allowedLegalKeys = map[string]bool{
	"terms":   true,
	"privacy": true,
	"refund":  true,
}

// LegalHandler serves admin-editable legal documents (terms, privacy, refund).
type LegalHandler struct {
	repo *repository.LegalRepository
}

// NewLegalHandler builds a LegalHandler.
func NewLegalHandler(repo *repository.LegalRepository) *LegalHandler {
	return &LegalHandler{repo: repo}
}

// Public returns a legal document for the app (no auth).
//
// GET /api/v1/legal/:key
func (h *LegalHandler) Public(c *fiber.Ctx) error {
	return h.fetch(c)
}

// Get returns a legal document for the admin editor.
//
// GET /api/v1/admin/legal/:key
func (h *LegalHandler) Get(c *fiber.Ctx) error {
	return h.fetch(c)
}

func (h *LegalHandler) fetch(c *fiber.Ctx) error {
	key := strings.ToLower(strings.TrimSpace(c.Params("key")))
	if !allowedLegalKeys[key] {
		return fiber.NewError(fiber.StatusNotFound, "unknown document")
	}
	doc, err := h.repo.Get(key)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "document not found")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "could not load the document")
	}
	return c.JSON(fiber.Map{"success": true, "document": doc})
}

type legalUpdateRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Update saves a legal document (admin).
//
// PUT /api/v1/admin/legal/:key  { "title": "...", "content": "..." }
func (h *LegalHandler) Update(c *fiber.Ctx) error {
	key := strings.ToLower(strings.TrimSpace(c.Params("key")))
	if !allowedLegalKeys[key] {
		return fiber.NewError(fiber.StatusNotFound, "unknown document")
	}
	var req legalUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Content) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "content is required")
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Terms & Conditions"
	}
	doc, err := h.repo.Upsert(key, title, req.Content)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not save the document")
	}
	return c.JSON(fiber.Map{"success": true, "document": doc})
}
