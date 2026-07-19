package handler

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// identifiable is implemented by landing list models so the generic handler can
// force the id from the URL path.
type identifiable interface {
	SetID(uint)
}

// LandingCrudHandler is a generic admin CRUD handler for a landing list entity.
// PT is the *T pointer type, constrained to implement identifiable.
type LandingCrudHandler[T any, PT interface {
	*T
	identifiable
}] struct {
	repo   *repository.OrderedRepo[T]
	entity string
}

// NewLandingCrudHandler builds a generic CRUD handler for entity type T.
func NewLandingCrudHandler[T any, PT interface {
	*T
	identifiable
}](repo *repository.OrderedRepo[T], entity string) *LandingCrudHandler[T, PT] {
	return &LandingCrudHandler[T, PT]{repo: repo, entity: entity}
}

// List returns all rows. GET .../<entity>
func (h *LandingCrudHandler[T, PT]) List(c *fiber.Ctx) error {
	items, err := h.repo.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load "+h.entity)
	}
	return c.JSON(fiber.Map{"success": true, "items": items})
}

// Create inserts a new row. POST .../<entity>
func (h *LandingCrudHandler[T, PT]) Create(c *fiber.Ctx) error {
	var item T
	if err := c.BodyParser(&item); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	PT(&item).SetID(0) // server assigns the id
	if err := h.repo.Create(&item); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create "+h.entity)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "item": item})
}

// Update edits a row. PUT .../<entity>/:id
func (h *LandingCrudHandler[T, PT]) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	// Load existing so unset fields (e.g. created_at) are preserved.
	existing, err := h.repo.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, h.entity)
	}
	if err := c.BodyParser(existing); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	PT(existing).SetID(id) // the path id always wins
	if err := h.repo.Update(existing); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update "+h.entity)
	}
	return c.JSON(fiber.Map{"success": true, "item": existing})
}

// Delete removes a row. DELETE .../<entity>/:id
func (h *LandingCrudHandler[T, PT]) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.repo.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete "+h.entity)
	}
	return c.JSON(fiber.Map{"success": true})
}

// LandingTextHandler handles the singleton landing text (GET/PUT).
type LandingTextHandler struct {
	repo *repository.LandingTextRepo
}

// NewLandingTextHandler builds a LandingTextHandler.
func NewLandingTextHandler(repo *repository.LandingTextRepo) *LandingTextHandler {
	return &LandingTextHandler{repo: repo}
}

// Get returns the landing text. GET /api/v1/admin/landing/text
func (h *LandingTextHandler) Get(c *fiber.Ctx) error {
	t, err := h.repo.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load text")
	}
	return c.JSON(fiber.Map{"success": true, "text": t})
}

// Update saves the landing text. PUT /api/v1/admin/landing/text
func (h *LandingTextHandler) Update(c *fiber.Ctx) error {
	existing, err := h.repo.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load text")
	}
	if err := c.BodyParser(existing); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	existing.ID = 1
	if err := h.repo.Save(existing); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to save text")
	}
	return c.JSON(fiber.Map{"success": true, "text": existing})
}

// Compile-time assertions that the models satisfy identifiable via pointers.
var (
	_ identifiable = (*model.LandingNavItem)(nil)
	_ identifiable = (*model.LandingStat)(nil)
	_ identifiable = (*model.LandingFeature)(nil)
	_ identifiable = (*model.LandingTestimonial)(nil)
	_ identifiable = (*model.LandingFaq)(nil)
)
