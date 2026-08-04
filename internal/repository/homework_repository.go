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

// CreateTest saves a graded test attempt.
func (r *HomeworkRepository) CreateTest(t *model.HomeworkTest) error {
	return r.db.Create(t).Error
}

// TestsForHomework returns the test attempts for a homework, scoped to the
// student (newest first) — used by the marks/report.
func (r *HomeworkRepository) TestsForHomework(homeworkID, studentID uint) ([]model.HomeworkTest, error) {
	var out []model.HomeworkTest
	err := r.db.
		Where("homework_id = ? AND student_id = ?", homeworkID, studentID).
		Order("created_at DESC").
		Find(&out).Error
	return out, err
}

// SetTaskStatus updates one task's status (pending|done|skipped), scoped to the
// student via the parent homework, then recomputes the homework's own status
// (done when every task is done/skipped, in_progress once any task is acted on)
// and touches its updated_at so the change flows through delta sync. Returns the
// refreshed homework with its tasks.
func (r *HomeworkRepository) SetTaskStatus(taskID, studentID uint, status string) (*model.Homework, error) {
	var task model.HomeworkTask
	err := r.db.
		Joins("JOIN homeworks ON homeworks.id = homework_tasks.homework_id").
		Where("homework_tasks.id = ? AND homeworks.student_id = ?", taskID, studentID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := r.db.Model(&model.HomeworkTask{}).
		Where("id = ?", task.ID).
		Update("status", status).Error; err != nil {
		return nil, err
	}

	// Recompute the parent homework's status from all its tasks.
	var tasks []model.HomeworkTask
	if err := r.db.Where("homework_id = ?", task.HomeworkID).Find(&tasks).Error; err != nil {
		return nil, err
	}
	hwStatus := "new"
	if len(tasks) > 0 {
		allSettled, anyActed := true, false
		for _, t := range tasks {
			if t.Status == "pending" {
				allSettled = false
			} else {
				anyActed = true
			}
		}
		switch {
		case allSettled:
			hwStatus = "done"
		case anyActed:
			hwStatus = "in_progress"
		}
	}
	if err := r.db.Model(&model.Homework{}).
		Where("id = ?", task.HomeworkID).
		Updates(map[string]any{"status": hwStatus, "updated_at": time.Now()}).Error; err != nil {
		return nil, err
	}
	return r.GetForStudent(task.HomeworkID, studentID)
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
