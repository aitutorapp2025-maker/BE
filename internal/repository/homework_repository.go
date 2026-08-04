package repository

import (
	"errors"
	"time"

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

// ChangedForStudent returns the student's homeworks (with tasks) whose updated_at
// is newer than `since` — the delta the app pulls to keep its local DB in sync.
// A zero `since` returns everything (first sync / after the user clears data).
func (r *HomeworkRepository) ChangedForStudent(studentID uint, since time.Time) ([]model.Homework, error) {
	var out []model.Homework
	q := r.db.
		Preload("Tasks", func(db *gorm.DB) *gorm.DB { return db.Order("order_no ASC") }).
		Where("student_id = ?", studentID)
	if !since.IsZero() {
		q = q.Where("updated_at > ?", since)
	}
	err := q.Order("created_at DESC").Find(&out).Error
	return out, err
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
