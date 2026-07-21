package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// TeachingLanguageHandler exposes the teaching-language master: a public list
// (for the app profile screen) and admin CRUD.
type TeachingLanguageHandler struct {
	repo *repository.TeachingLanguageRepository
}

// NewTeachingLanguageHandler builds a TeachingLanguageHandler.
func NewTeachingLanguageHandler(repo *repository.TeachingLanguageRepository) *TeachingLanguageHandler {
	return &TeachingLanguageHandler{repo: repo}
}

type teachingLanguageRequest struct {
	Name   string `json:"name"`
	Active *bool  `json:"active"`
}

// Public returns the active teaching languages for the app.
// GET /api/v1/teaching-languages
func (h *TeachingLanguageHandler) Public(c *fiber.Ctx) error {
	langs, err := h.repo.ListActive()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load teaching languages")
	}
	return c.JSON(fiber.Map{"success": true, "teaching_languages": langs})
}

// List returns all teaching languages (admin). GET /api/v1/admin/teaching-languages
func (h *TeachingLanguageHandler) List(c *fiber.Ctx) error {
	langs, err := h.repo.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load teaching languages")
	}
	return c.JSON(fiber.Map{"success": true, "teaching_languages": langs})
}

// Create adds a teaching language. POST /api/v1/admin/teaching-languages
func (h *TeachingLanguageHandler) Create(c *fiber.Ctx) error {
	req, err := parseTeachingLanguageBody(c)
	if err != nil {
		return err
	}
	l := &model.TeachingLanguage{Name: req.Name, Active: true}
	if req.Active != nil {
		l.Active = *req.Active
	}
	if err := h.repo.Create(l); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create teaching language")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "teaching_language": l})
}

// Update edits a teaching language. PUT /api/v1/admin/teaching-languages/:id
func (h *TeachingLanguageHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	l, err := h.repo.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "teaching language")
	}
	req, err := parseTeachingLanguageBody(c)
	if err != nil {
		return err
	}
	l.Name = req.Name
	if req.Active != nil {
		l.Active = *req.Active
	}
	if err := h.repo.Update(l); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update teaching language")
	}
	return c.JSON(fiber.Map{"success": true, "teaching_language": l})
}

// Delete removes a teaching language. DELETE /api/v1/admin/teaching-languages/:id
func (h *TeachingLanguageHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.repo.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete teaching language")
	}
	return c.JSON(fiber.Map{"success": true})
}

func parseTeachingLanguageBody(c *fiber.Ctx) (*teachingLanguageRequest, error) {
	var req teachingLanguageRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	return &req, nil
}
