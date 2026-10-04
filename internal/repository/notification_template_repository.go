package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// NotificationTemplateRepository provides access to admin-editable notification
// wording.
type NotificationTemplateRepository struct{ db *gorm.DB }

// NewNotificationTemplateRepository builds a NotificationTemplateRepository.
func NewNotificationTemplateRepository(db *gorm.DB) *NotificationTemplateRepository {
	return &NotificationTemplateRepository{db: db}
}

// List returns all templates (one per type).
func (r *NotificationTemplateRepository) List() ([]model.NotificationTemplate, error) {
	var out []model.NotificationTemplate
	return out, r.db.Order("type ASC").Find(&out).Error
}

// Get returns the template for a type, or nil (not an error) when none exists.
func (r *NotificationTemplateRepository) Get(typ string) (*model.NotificationTemplate, error) {
	var t model.NotificationTemplate
	err := r.db.Where("type = ?", typ).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Upsert creates or updates the template for a type.
func (r *NotificationTemplateRepository) Upsert(typ, title, body string, active bool) (*model.NotificationTemplate, error) {
	var t model.NotificationTemplate
	err := r.db.Where("type = ?", typ).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		t = model.NotificationTemplate{Type: typ}
	} else if err != nil {
		return nil, err
	}
	t.Title = title
	t.Body = body
	t.Active = active
	if t.ID == 0 {
		return &t, r.db.Create(&t).Error
	}
	return &t, r.db.Save(&t).Error
}
