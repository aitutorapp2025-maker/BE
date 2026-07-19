package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// StudentRepository provides data access for Student records.
type StudentRepository struct {
	db *gorm.DB
}

// NewStudentRepository builds a StudentRepository.
func NewStudentRepository(db *gorm.DB) *StudentRepository {
	return &StudentRepository{db: db}
}

// List returns all students, newest first.
func (r *StudentRepository) List() ([]model.Student, error) {
	var students []model.Student
	err := r.db.Order("created_at DESC").Find(&students).Error
	return students, err
}

// FindByID returns a student by id, or ErrNotFound.
func (r *StudentRepository) FindByID(id uint) (*model.Student, error) {
	var s model.Student
	err := r.db.First(&s, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// Create inserts a new student.
func (r *StudentRepository) Create(s *model.Student) error {
	return r.db.Create(s).Error
}

// Update saves changes to an existing student.
func (r *StudentRepository) Update(s *model.Student) error {
	return r.db.Save(s).Error
}

// Delete removes a student by id.
func (r *StudentRepository) Delete(id uint) error {
	return r.db.Delete(&model.Student{}, id).Error
}
