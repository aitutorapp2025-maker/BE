package repository

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// SettingRepository provides access to the singleton settings row (id = 1).
type SettingRepository struct {
	db *gorm.DB
}

// NewSettingRepository builds a SettingRepository.
func NewSettingRepository(db *gorm.DB) *SettingRepository {
	return &SettingRepository{db: db}
}

// Get returns the settings row, creating a default one if none exists.
func (r *SettingRepository) Get() (*model.Setting, error) {
	var s model.Setting
	err := r.db.First(&s, 1).Error
	if err == gorm.ErrRecordNotFound {
		s = model.Setting{ID: 1, AppName: "Vaha AI", EmailNotifications: true}
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

// Save persists the settings row.
func (r *SettingRepository) Save(s *model.Setting) error {
	s.ID = 1
	return r.db.Save(s).Error
}
