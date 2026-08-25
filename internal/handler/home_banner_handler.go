package handler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// HomeBannerHandler serves the app's Home-screen banners: a public active list
// for the student app plus the admin CRUD behind the banners permission.
type HomeBannerHandler struct {
	banners *repository.HomeBannerRepository
}

// NewHomeBannerHandler builds a HomeBannerHandler.
func NewHomeBannerHandler(banners *repository.HomeBannerRepository) *HomeBannerHandler {
	return &HomeBannerHandler{banners: banners}
}

// Public returns the active banners for the app Home screen (cached).
// GET /api/v1/banners
func (h *HomeBannerHandler) Public(c *fiber.Ctx) error {
	banners, err := h.banners.ListActive()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load banners")
	}
	return c.JSON(fiber.Map{"success": true, "banners": banners})
}

// List returns every banner for the admin page.
// GET /admin/banners
func (h *HomeBannerHandler) List(c *fiber.Ctx) error {
	banners, err := h.banners.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load banners")
	}
	return c.JSON(fiber.Map{"success": true, "banners": banners})
}

type bannerRequest struct {
	Title     string `json:"title"`
	Message   string `json:"message"`
	ImageURL  string `json:"image_url"`
	Active    bool   `json:"active"`
	SortOrder int    `json:"sort_order"`
}

// validate trims the fields and requires SOME content (text or image).
func (b *bannerRequest) validate() error {
	b.Title = strings.TrimSpace(b.Title)
	b.Message = strings.TrimSpace(b.Message)
	b.ImageURL = strings.TrimSpace(b.ImageURL)
	if b.Title == "" && b.Message == "" && b.ImageURL == "" {
		return errors.New("add an image, some text, or both")
	}
	return nil
}

// Create adds a banner. POST /admin/banners
func (h *HomeBannerHandler) Create(c *fiber.Ctx) error {
	var req bannerRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := req.validate(); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	banner := &model.HomeBanner{
		Title:     req.Title,
		Message:   req.Message,
		ImageURL:  req.ImageURL,
		Active:    req.Active,
		SortOrder: req.SortOrder,
	}
	if err := h.banners.Create(banner); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create banner")
	}
	return c.JSON(fiber.Map{"success": true, "banner": banner})
}

// Update edits a banner. PUT /admin/banners/:id
func (h *HomeBannerHandler) Update(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid banner id")
	}
	banner, err := h.banners.FindByID(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "banner not found")
	}
	var req bannerRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := req.validate(); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	banner.Title = req.Title
	banner.Message = req.Message
	banner.ImageURL = req.ImageURL
	banner.Active = req.Active
	banner.SortOrder = req.SortOrder
	if err := h.banners.Update(banner); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update banner")
	}
	return c.JSON(fiber.Map{"success": true, "banner": banner})
}

// Delete removes a banner. DELETE /admin/banners/:id
func (h *HomeBannerHandler) Delete(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid banner id")
	}
	if err := h.banners.Delete(uint(id)); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete banner")
	}
	return c.JSON(fiber.Map{"success": true})
}
