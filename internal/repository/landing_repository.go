package repository

import (
	"context"
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// bustLanding clears the cached public landing aggregate (see LandingHandler).
// All landing writes go through these repos, so busting here keeps the cached
// page fresh without the handlers needing to know about the cache.
func bustLanding(c *cache.Store) {
	c.Del(context.Background(), cache.KeyLandingPublic)
}

// OrderedRepo is a generic repository for landing list entities ordered by
// sort_order (nav items, stats, features, testimonials, faqs).
type OrderedRepo[T any] struct {
	db    *gorm.DB
	cache *cache.Store
}

// NewOrderedRepo builds an OrderedRepo for type T. cache may be nil.
func NewOrderedRepo[T any](db *gorm.DB, c *cache.Store) *OrderedRepo[T] {
	return &OrderedRepo[T]{db: db, cache: c}
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
	if err := r.db.Create(item).Error; err != nil {
		return err
	}
	bustLanding(r.cache)
	return nil
}

// Update saves an existing row.
func (r *OrderedRepo[T]) Update(item *T) error {
	if err := r.db.Save(item).Error; err != nil {
		return err
	}
	bustLanding(r.cache)
	return nil
}

// Delete removes a row by id.
func (r *OrderedRepo[T]) Delete(id uint) error {
	var item T
	if err := r.db.Delete(&item, id).Error; err != nil {
		return err
	}
	bustLanding(r.cache)
	return nil
}

// LandingTextRepo provides access to the singleton landing text row (id=1).
type LandingTextRepo struct {
	db    *gorm.DB
	cache *cache.Store
}

// NewLandingTextRepo builds a LandingTextRepo. cache may be nil.
func NewLandingTextRepo(db *gorm.DB, c *cache.Store) *LandingTextRepo {
	return &LandingTextRepo{db: db, cache: c}
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
	if err := r.db.Save(t).Error; err != nil {
		return err
	}
	bustLanding(r.cache)
	return nil
}

// LandingSeoRepo provides access to the singleton landing SEO row (id=1).
type LandingSeoRepo struct {
	db    *gorm.DB
	cache *cache.Store
}

// NewLandingSeoRepo builds a LandingSeoRepo. cache may be nil.
func NewLandingSeoRepo(db *gorm.DB, c *cache.Store) *LandingSeoRepo {
	return &LandingSeoRepo{db: db, cache: c}
}

// Get returns the SEO row, creating a default if none exists.
func (r *LandingSeoRepo) Get() (*model.LandingSeo, error) {
	var s model.LandingSeo
	err := r.db.First(&s, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		s = model.LandingSeo{ID: 1}
		if err := r.db.Create(&s).Error; err != nil {
			return nil, err
		}
		return &s, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Save persists the SEO row (always id=1).
func (r *LandingSeoRepo) Save(s *model.LandingSeo) error {
	s.ID = 1
	if err := r.db.Save(s).Error; err != nil {
		return err
	}
	bustLanding(r.cache)
	return nil
}
