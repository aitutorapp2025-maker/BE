package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// OrderedRepo is a generic repository for landing list entities ordered by
// sort_order (nav items, stats, features, testimonials, faqs).
type OrderedRepo[T any] struct {
	db *gorm.DB
}

// NewOrderedRepo builds an OrderedRepo for type T.
func NewOrderedRepo[T any](db *gorm.DB) *OrderedRepo[T] {
	return &OrderedRepo[T]{db: db}
}

// List returns all rows ordered by sort_order then id.
func (r *OrderedRepo[T]) List() ([]T, error) {
	var items []T
	err := r.db.Order("sort_order ASC, id ASC").Find(&items).Error
	return items, err
}

// FindByID returns a row by id, or ErrNotFound.
func (r *OrderedRepo[T]) FindByID(id uint) (*T, error) {
	var item T
	err := r.db.First(&item, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// Create inserts a new row.
func (r *OrderedRepo[T]) Create(item *T) error {
	return r.db.Create(item).Error
}

// Update saves an existing row.
func (r *OrderedRepo[T]) Update(item *T) error {
	return r.db.Save(item).Error
}

// Delete removes a row by id.
func (r *OrderedRepo[T]) Delete(id uint) error {
	var item T
	return r.db.Delete(&item, id).Error
}

// LandingTextRepo provides access to the singleton landing text row (id=1).
type LandingTextRepo struct {
	db *gorm.DB
}

// NewLandingTextRepo builds a LandingTextRepo.
func NewLandingTextRepo(db *gorm.DB) *LandingTextRepo {
	return &LandingTextRepo{db: db}
}

// Get returns the text row, creating a default if none exists.
func (r *LandingTextRepo) Get() (*model.LandingText, error) {
	var t model.LandingText
	err := r.db.First(&t, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		t = model.LandingText{ID: 1}
		if err := r.db.Create(&t).Error; err != nil {
			return nil, err
		}
		return &t, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Save persists the text row (always id=1).
func (r *LandingTextRepo) Save(t *model.LandingText) error {
	t.ID = 1
	return r.db.Save(t).Error
}
