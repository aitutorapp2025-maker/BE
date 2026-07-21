package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// LegalRepository provides data access for admin-editable legal documents.
type LegalRepository struct {
	db *gorm.DB
}

// NewLegalRepository builds a LegalRepository.
func NewLegalRepository(db *gorm.DB) *LegalRepository {
	return &LegalRepository{db: db}
}

// Get returns a legal document by key, or ErrNotFound.
func (r *LegalRepository) Get(key string) (*model.LegalDocument, error) {
	var d model.LegalDocument
	err := r.db.Where("key = ?", key).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Upsert creates or updates the document for a key and returns it.
func (r *LegalRepository) Upsert(key, title, content string) (*model.LegalDocument, error) {
	var d model.LegalDocument
	err := r.db.Where("key = ?", key).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		d = model.LegalDocument{Key: key, Title: title, Content: content}
		if e := r.db.Create(&d).Error; e != nil {
			return nil, e
		}
		return &d, nil
	}
	if err != nil {
		return nil, err
	}
	d.Title = title
	d.Content = content
	if e := r.db.Save(&d).Error; e != nil {
		return nil, e
	}
	return &d, nil
}
