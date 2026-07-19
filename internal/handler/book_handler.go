package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// BookHandler exposes CRUD endpoints for books (admin only).
type BookHandler struct {
	books *repository.BookRepository
}

// NewBookHandler builds a BookHandler.
func NewBookHandler(books *repository.BookRepository) *BookHandler {
	return &BookHandler{books: books}
}

type bookRequest struct {
	Title     string `json:"title"`
	ClassName string `json:"class_name"`
	Subject   string `json:"subject"`
	Medium    string `json:"medium"`
	Publisher string `json:"publisher"`
	Status    string `json:"status"`
}

// List returns all books, optionally filtered by ?class_name= and ?medium=.
// GET /api/v1/admin/books
func (h *BookHandler) List(c *fiber.Ctx) error {
	className := strings.TrimSpace(c.Query("class_name"))
	medium := strings.TrimSpace(c.Query("medium"))

	var (
		books []model.Book
		err   error
	)
	if className != "" {
		books, err = h.books.ListByClass(className, medium)
	} else {
		books, err = h.books.List()
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load books")
	}
	return c.JSON(fiber.Map{"success": true, "books": books})
}

// Get returns a single book. GET /api/v1/admin/books/:id
func (h *BookHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	b, err := h.books.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "book")
	}
	return c.JSON(fiber.Map{"success": true, "book": b})
}

// Create adds a book. POST /api/v1/admin/books
func (h *BookHandler) Create(c *fiber.Ctx) error {
	req, err := parseBookBody(c)
	if err != nil {
		return err
	}
	b := &model.Book{
		Title: req.Title, ClassName: req.ClassName, Subject: req.Subject,
		Medium: req.Medium, Publisher: req.Publisher, Status: req.Status,
	}
	if err := h.books.Create(b); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create book")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "book": b})
}

// Update edits a book. PUT /api/v1/admin/books/:id
func (h *BookHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	b, err := h.books.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "book")
	}
	req, err := parseBookBody(c)
	if err != nil {
		return err
	}
	b.Title = req.Title
	b.ClassName = req.ClassName
	b.Subject = req.Subject
	b.Medium = req.Medium
	b.Publisher = req.Publisher
	b.Status = req.Status
	if err := h.books.Update(b); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update book")
	}
	return c.JSON(fiber.Map{"success": true, "book": b})
}

// Delete removes a book. DELETE /api/v1/admin/books/:id
func (h *BookHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.books.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete book")
	}
	return c.JSON(fiber.Map{"success": true})
}

func parseBookBody(c *fiber.Ctx) (*bookRequest, error) {
	var req bookRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Title = strings.TrimSpace(req.Title)
	req.ClassName = strings.TrimSpace(req.ClassName)
	if req.Title == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest, "title is required")
	}
	if req.ClassName == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest, "class_name is required")
	}
	if strings.TrimSpace(req.Medium) == "" {
		req.Medium = "English"
	}
	if strings.TrimSpace(req.Status) == "" {
		req.Status = "Pending"
	}
	return &req, nil
}
