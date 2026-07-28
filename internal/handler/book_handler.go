package handler

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// BookHandler exposes CRUD endpoints for books (admin only), plus the ingestion
// trigger that indexes a book's text into the vector store.
type BookHandler struct {
	books  *repository.BookRepository
	ingest *ai.IngestPublisher
}

// NewBookHandler builds a BookHandler. The ingest publisher may be a no-op
// publisher when the AI keys aren't configured.
func NewBookHandler(books *repository.BookRepository, ingest *ai.IngestPublisher) *BookHandler {
	return &BookHandler{books: books, ingest: ingest}
}

type ingestRequest struct {
	Content string `json:"content"`
}

// Ingest queues a book's text for indexing into the vector store. The book's
// status flips to Processing and the background worker embeds + stores chunks.
// POST /api/v1/admin/books/:id/ingest  { "content": "<full book text>" }
func (h *BookHandler) Ingest(c *fiber.Ctx) error {
	if !h.ingest.Enabled() {
		return fiber.NewError(fiber.StatusServiceUnavailable,
			"AI is not configured on the server (missing ANTHROPIC_API_KEY / VOYAGE_API_KEY)")
	}
	id, err := parseID(c)
	if err != nil {
		return err
	}
	book, err := h.books.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "book")
	}
	var req ingestRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Content) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "content is required")
	}
	if err := h.ingest.Enqueue(ai.IngestJob{BookID: book.ID, Content: req.Content}); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not queue the book for indexing")
	}
	// Reflect the queued state immediately so the admin sees progress.
	book.Status = service.BookStatusProcessing
	_ = h.books.Update(book)
	return c.JSON(fiber.Map{"success": true, "status": book.Status})
}

type bookRequest struct {
	Title     string `json:"title"`
	ClassName string `json:"class_name"`
	Subject   string `json:"subject"`
	Medium    string `json:"medium"`
	Publisher string `json:"publisher"`
	Status    string `json:"status"`
	// Content is the book's text. When present on create/update it's queued for
	// background indexing (RabbitMQ). Not stored on the book row — the chunks
	// table is its indexed form. Empty on a metadata-only edit (keeps the
	// existing index).
	Content string `json:"content"`
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
	h.autoIngest(b, req.Content)
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
	// Re-index in the background only when new text was supplied.
	h.autoIngest(b, req.Content)
	return c.JSON(fiber.Map{"success": true, "book": b})
}

// autoIngest queues the book's text for background indexing (RabbitMQ) when
// content was provided and the AI pipeline is configured. Best-effort: a queue
// failure does not fail the save — the metadata is already persisted, and the
// admin can re-trigger from the book page. Flips the book's status to Processing
// so the UI reflects that indexing is underway.
func (h *BookHandler) autoIngest(b *model.Book, content string) {
	content = strings.TrimSpace(content)
	if content == "" || !h.ingest.Enabled() {
		return
	}
	if err := h.ingest.Enqueue(ai.IngestJob{BookID: b.ID, Content: content}); err != nil {
		return
	}
	b.Status = service.BookStatusProcessing
	_ = h.books.Update(b)
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
