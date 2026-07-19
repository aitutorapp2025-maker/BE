package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// ContactRepository provides data access for contact submissions.
type ContactRepository struct {
	db *gorm.DB
}

// NewContactRepository builds a ContactRepository.
func NewContactRepository(db *gorm.DB) *ContactRepository {
	return &ContactRepository{db: db}
}

// List returns all submissions, newest first.
func (r *ContactRepository) List() ([]model.ContactMessage, error) {
	var items []model.ContactMessage
	err := r.db.Order("created_at DESC").Find(&items).Error
	return items, err
}

// CountNew returns the number of unread submissions.
func (r *ContactRepository) CountNew() (int64, error) {
	var n int64
	err := r.db.Model(&model.ContactMessage{}).Where("status = ?", "new").Count(&n).Error
	return n, err
}

// FindByID returns a submission by id, or ErrNotFound.
func (r *ContactRepository) FindByID(id uint) (*model.ContactMessage, error) {
	var m model.ContactMessage
	err := r.db.First(&m, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// Create inserts a new submission.
func (r *ContactRepository) Create(m *model.ContactMessage) error {
	return r.db.Create(m).Error
}

// MarkRead sets a submission's status to "read".
func (r *ContactRepository) MarkRead(id uint) error {
	return r.db.Model(&model.ContactMessage{}).Where("id = ?", id).
		Update("status", "read").Error
}

// Delete removes a submission by id.
func (r *ContactRepository) Delete(id uint) error {
	return r.db.Delete(&model.ContactMessage{}, id).Error
}
