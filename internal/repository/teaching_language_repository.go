package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// TeachingLanguageRepository provides data access for the teaching-language master.
type TeachingLanguageRepository struct {
	db *gorm.DB
}

// NewTeachingLanguageRepository builds a TeachingLanguageRepository.
func NewTeachingLanguageRepository(db *gorm.DB) *TeachingLanguageRepository {
	return &TeachingLanguageRepository{db: db}
}

// List returns all teaching languages (admin), oldest first.
func (r *TeachingLanguageRepository) List() ([]model.TeachingLanguage, error) {
	var langs []model.TeachingLanguage
	err := r.db.Order("id ASC").Find(&langs).Error
	return langs, err
}

// ListActive returns only the enabled teaching languages (for the app).
func (r *TeachingLanguageRepository) ListActive() ([]model.TeachingLanguage, error) {
	var langs []model.TeachingLanguage
	err := r.db.Where("active = ?", true).Order("id ASC").Find(&langs).Error
	return langs, err
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
	return r.db.Create(l).Error
}

// Update saves changes to a teaching language.
func (r *TeachingLanguageRepository) Update(l *model.TeachingLanguage) error {
	return r.db.Save(l).Error
}

// Delete removes a teaching language by id.
func (r *TeachingLanguageRepository) Delete(id uint) error {
	return r.db.Delete(&model.TeachingLanguage{}, id).Error
}
