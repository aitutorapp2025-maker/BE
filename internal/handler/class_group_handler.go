package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// ClassGroupHandler exposes the class-group master: a public list (for the app
// profile screen, filtered by the student's class + board) and admin CRUD.
type ClassGroupHandler struct {
	repo *repository.ClassGroupRepository
}

// NewClassGroupHandler builds a ClassGroupHandler.
func NewClassGroupHandler(repo *repository.ClassGroupRepository) *ClassGroupHandler {
	return &ClassGroupHandler{repo: repo}
}

type classGroupRequest struct {
	ClassName string `json:"class_name"`
	Board     string `json:"board"`
	Name      string `json:"name"`
	SortOrder *int   `json:"sort_order"`
	Active    *bool  `json:"active"`
}

// Public returns the active groups a student may pick.
// GET /api/v1/class-groups?class=Class%2011&board=State%20Board
//
// Without a class filter it returns every active group, so the app can cache
// the whole list once and filter locally as the student switches class/board.
func (h *ClassGroupHandler) Public(c *fiber.Ctx) error {
	groups, err := h.repo.ListActive(
		strings.TrimSpace(c.Query("class")),
		strings.TrimSpace(c.Query("board")),
	)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load class groups")
	}
	return c.JSON(fiber.Map{"success": true, "class_groups": groups})
}

// List returns all groups (admin), optionally filtered to one class.
// GET /api/v1/admin/class-groups?class=Class%2011
func (h *ClassGroupHandler) List(c *fiber.Ctx) error {
	var (
		groups []model.ClassGroup
		err    error
	)
	if className := strings.TrimSpace(c.Query("class")); className != "" {
		groups, err = h.repo.ListByClass(className)
	} else {
		groups, err = h.repo.List()
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load class groups")
	}
	return c.JSON(fiber.Map{"success": true, "class_groups": groups})
}

// Create adds a group. POST /api/v1/admin/class-groups
func (h *ClassGroupHandler) Create(c *fiber.Ctx) error {
	req, err := parseClassGroupBody(c)
	if err != nil {
		return err
	}
	g := &model.ClassGroup{
		ClassName: req.ClassName,
		Board:     req.Board,
		Name:      req.Name,
		Active:    true,
	}
	if req.SortOrder != nil {
		g.SortOrder = *req.SortOrder
	}
	if req.Active != nil {
		g.Active = *req.Active
	}
	if err := h.repo.Create(g); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create class group")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "class_group": g})
}

// Update edits a group. PUT /api/v1/admin/class-groups/:id
func (h *ClassGroupHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	g, err := h.repo.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "class group")
	}
	req, err := parseClassGroupBody(c)
	if err != nil {
		return err
	}
	g.ClassName = req.ClassName
	g.Board = req.Board
	g.Name = req.Name
	if req.SortOrder != nil {
		g.SortOrder = *req.SortOrder
	}
	if req.Active != nil {
		g.Active = *req.Active
	}
	if err := h.repo.Update(g); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update class group")
	}
	return c.JSON(fiber.Map{"success": true, "class_group": g})
}

// Delete removes a group. DELETE /api/v1/admin/class-groups/:id
func (h *ClassGroupHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.repo.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete class group")
	}
	return c.JSON(fiber.Map{"success": true})
}

func parseClassGroupBody(c *fiber.Ctx) (*classGroupRequest, error) {
	var req classGroupRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.ClassName = strings.TrimSpace(req.ClassName)
	req.Board = strings.TrimSpace(req.Board)
	req.Name = strings.TrimSpace(req.Name)
	if req.ClassName == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest, "class_name is required")
	}
	if req.Name == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	return &req, nil
}
