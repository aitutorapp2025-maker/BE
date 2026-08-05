package repository

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// NotificationRepository stores the delivered-notification feed.
type NotificationRepository struct {
	db *gorm.DB
}

// NewNotificationRepository builds a NotificationRepository.
func NewNotificationRepository(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// CreateBatch inserts one or more notification rows. No-op on an empty slice.
func (r *NotificationRepository) CreateBatch(notifs []model.Notification) error {
	if len(notifs) == 0 {
		return nil
	}
	return r.db.CreateInBatches(notifs, 100).Error
}

// FeedForStudent returns the notifications visible to a student — broadcasts
// (student_id = 0) plus any targeted at them — newest first, capped at limit.
func (r *NotificationRepository) FeedForStudent(studentID uint, limit int) ([]model.Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []model.Notification
	err := r.db.
		Where("student_id = 0 OR student_id = ?", studentID).
		Order("created_at DESC").
		Limit(limit).
		Find(&out).Error
	return out, err
}
