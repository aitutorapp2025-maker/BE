package repository

import (
	"context"
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// TeachingLanguageRepository provides data access for the teaching-language
// master. The active list is read on the app profile screen, so it's cached
// (no expiry) and busted on any create/update/delete.
type TeachingLanguageRepository struct {
	db    *gorm.DB
	cache *cache.Store
}

// NewTeachingLanguageRepository builds a TeachingLanguageRepository. cache may
// be nil (caching off).
func NewTeachingLanguageRepository(db *gorm.DB, c *cache.Store) *TeachingLanguageRepository {
	return &TeachingLanguageRepository{db: db, cache: c}
}

// bust clears the cached active-languages list (call after any write).
func (r *TeachingLanguageRepository) bust() {
	r.cache.Del(context.Background(), cache.KeyTeachLangsActive)
}

// List returns all teaching languages (admin), oldest first.
func (r *TeachingLanguageRepository) List() ([]model.TeachingLanguage, error) {
	var langs []model.TeachingLanguage
	err := r.db.Order("id ASC").Find(&langs).Error
	return langs, err
}

// ListActive returns only the enabled teaching languages (for the app), cached.
func (r *TeachingLanguageRepository) ListActive() ([]model.TeachingLanguage, error) {
	ctx := context.Background()
	var langs []model.TeachingLanguage
	if r.cache.Get(ctx, cache.KeyTeachLangsActive, &langs) {
		return langs, nil
	}
	if err := r.db.Where("active = ?", true).Order("id ASC").Find(&langs).Error; err != nil {
		return nil, err
	}
	r.cache.Set(ctx, cache.KeyTeachLangsActive, langs)
	return langs, nil
}

// FindByID returns a teaching language by id, or ErrNotFound.
func (r *TeachingLanguageRepository) FindByID(id uint) (*model.TeachingLanguage, error) {
	var l model.TeachingLanguage
	err := r.db.First(&l, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// Create inserts a new teaching language.
func (r *TeachingLanguageRepository) Create(l *model.TeachingLanguage) error {
	if err := r.db.Create(l).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}

// Update saves changes to a teaching language.
func (r *TeachingLanguageRepository) Update(l *model.TeachingLanguage) error {
	if err := r.db.Save(l).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}

// Delete removes a teaching language by id.
func (r *TeachingLanguageRepository) Delete(id uint) error {
	if err := r.db.Delete(&model.TeachingLanguage{}, id).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}
