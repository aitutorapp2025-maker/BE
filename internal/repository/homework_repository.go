package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// HomeworkRepository provides data access for homeworks and their tasks.
type HomeworkRepository struct {
	db *gorm.DB
}

// NewHomeworkRepository builds a HomeworkRepository.
func NewHomeworkRepository(db *gorm.DB) *HomeworkRepository {
	return &HomeworkRepository{db: db}
}

// Create inserts a homework together with its tasks (GORM cascades the slice).
func (r *HomeworkRepository) Create(hw *model.Homework) error {
	return r.db.Create(hw).Error
}

// GetForStudent returns a homework (with tasks, ordered) scoped to the student,
// so one student can never read another's homework.
func (r *HomeworkRepository) GetForStudent(id, studentID uint) (*model.Homework, error) {
	var hw model.Homework
	err := r.db.
		Preload("Tasks", func(db *gorm.DB) *gorm.DB { return db.Order("order_no ASC") }).
		Where("id = ? AND student_id = ?", id, studentID).
		First(&hw).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &hw, nil
}

// ListForStudent returns the student's homeworks (newest first) with their tasks.
func (r *HomeworkRepository) ListForStudent(studentID uint) ([]model.Homework, error) {
	var out []model.Homework
	err := r.db.
		Preload("Tasks", func(db *gorm.DB) *gorm.DB { return db.Order("order_no ASC") }).
		Where("student_id = ?", studentID).
		Order("created_at DESC").
		Find(&out).Error
	return out, err
}
