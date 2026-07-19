package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// ClassRepository provides data access for SchoolClass records.
type ClassRepository struct {
	db *gorm.DB
}

// NewClassRepository builds a ClassRepository.
func NewClassRepository(db *gorm.DB) *ClassRepository {
	return &ClassRepository{db: db}
}

// List returns all classes ordered by their number.
func (r *ClassRepository) List() ([]model.SchoolClass, error) {
	var classes []model.SchoolClass
	err := r.db.Order("number ASC").Find(&classes).Error
	return classes, err
}

// FindByID returns a class by id, or ErrNotFound.
func (r *ClassRepository) FindByID(id uint) (*model.SchoolClass, error) {
	var c model.SchoolClass
	err := r.db.First(&c, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Create inserts a new class.
func (r *ClassRepository) Create(c *model.SchoolClass) error {
	return r.db.Create(c).Error
}

// Update saves changes to an existing class.
func (r *ClassRepository) Update(c *model.SchoolClass) error {
	return r.db.Save(c).Error
}

// Delete removes a class by id.
func (r *ClassRepository) Delete(id uint) error {
	return r.db.Delete(&model.SchoolClass{}, id).Error
}
