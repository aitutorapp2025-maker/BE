package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// BookRepository provides data access for Book records.
type BookRepository struct {
	db *gorm.DB
}

// NewBookRepository builds a BookRepository.
func NewBookRepository(db *gorm.DB) *BookRepository {
	return &BookRepository{db: db}
}

// List returns all books, newest first.
func (r *BookRepository) List() ([]model.Book, error) {
	var books []model.Book
	err := r.db.Order("created_at DESC").Find(&books).Error
	return books, err
}

// ListByClass returns books for a given class name, optionally filtered by medium.
func (r *BookRepository) ListByClass(className, medium string) ([]model.Book, error) {
	var books []model.Book
	q := r.db.Where("class_name = ?", className)
	if medium != "" {
		q = q.Where("medium = ?", medium)
	}
	err := q.Order("created_at DESC").Find(&books).Error
	return books, err
}

// FindByID returns a book by id, or ErrNotFound.
func (r *BookRepository) FindByID(id uint) (*model.Book, error) {
	var b model.Book
	err := r.db.First(&b, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// Create inserts a new book.
func (r *BookRepository) Create(b *model.Book) error {
	return r.db.Create(b).Error
}

// Update saves changes to an existing book.
func (r *BookRepository) Update(b *model.Book) error {
	return r.db.Save(b).Error
}

// Delete removes a book by id.
func (r *BookRepository) Delete(id uint) error {
	return r.db.Delete(&model.Book{}, id).Error
}
