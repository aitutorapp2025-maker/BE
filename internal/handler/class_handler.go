package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// ClassHandler exposes CRUD endpoints for the class master (admin only).
type ClassHandler struct {
	classes *repository.ClassRepository
}

// NewClassHandler builds a ClassHandler.
func NewClassHandler(classes *repository.ClassRepository) *ClassHandler {
	return &ClassHandler{classes: classes}
}

type classRequest struct {
	Name   string `json:"name"`
	Number int    `json:"number"`
	Active *bool  `json:"active"`
}

// List returns all classes. GET /api/v1/admin/classes
func (h *ClassHandler) List(c *fiber.Ctx) error {
	classes, err := h.classes.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load classes")
	}
	return c.JSON(fiber.Map{"success": true, "classes": classes})
}

// Get returns a single class. GET /api/v1/admin/classes/:id
func (h *ClassHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	cls, err := h.classes.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "class")
	}
	return c.JSON(fiber.Map{"success": true, "class": cls})
}

// Create adds a class. POST /api/v1/admin/classes
func (h *ClassHandler) Create(c *fiber.Ctx) error {
	req, err := parseClassBody(c)
	if err != nil {
		return err
	}
	cls := &model.SchoolClass{Name: req.Name, Number: req.Number, Active: true}
	if req.Active != nil {
		cls.Active = *req.Active
	}
	if err := h.classes.Create(cls); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create class")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "class": cls})
}

// Update edits a class. PUT /api/v1/admin/classes/:id
func (h *ClassHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	cls, err := h.classes.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "class")
	}
	req, err := parseClassBody(c)
	if err != nil {
		return err
	}
	cls.Name = req.Name
	cls.Number = req.Number
	if req.Active != nil {
		cls.Active = *req.Active
	}
	if err := h.classes.Update(cls); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update class")
	}
	return c.JSON(fiber.Map{"success": true, "class": cls})
}

// Delete removes a class. DELETE /api/v1/admin/classes/:id
func (h *ClassHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.classes.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete class")
	}
	return c.JSON(fiber.Map{"success": true})
}

func parseClassBody(c *fiber.Ctx) (*classRequest, error) {
	var req classRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	return &req, nil
}
