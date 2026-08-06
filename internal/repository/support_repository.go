package repository

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// SupportRepository stores "Report a problem" tickets.
type SupportRepository struct {
	db *gorm.DB
}

// NewSupportRepository builds a SupportRepository.
func NewSupportRepository(db *gorm.DB) *SupportRepository {
	return &SupportRepository{db: db}
}

// Create inserts a new ticket.
func (r *SupportRepository) Create(t *model.SupportTicket) error {
	return r.db.Create(t).Error
}

// Update saves changes (admin reply / status).
func (r *SupportRepository) Update(t *model.SupportTicket) error {
	return r.db.Save(t).Error
}

// Get fetches one ticket by id.
func (r *SupportRepository) Get(id uint) (*model.SupportTicket, error) {
	var t model.SupportTicket
	err := r.db.First(&t, id).Error
	return &t, err
}

// ListByStudent returns a student's own tickets, newest first.
func (r *SupportRepository) ListByStudent(studentID uint) ([]model.SupportTicket, error) {
	var out []model.SupportTicket
	err := r.db.Where("student_id = ?", studentID).
		Order("created_at DESC").Find(&out).Error
	return out, err
}

// List returns all tickets for the admin, newest first, optionally filtered by
// status ("" = all), capped at limit.
func (r *SupportRepository) List(status string, limit int) ([]model.SupportTicket, error) {
	if limit <= 0 {
		limit = 200
	}
	q := r.db.Order("created_at DESC").Limit(limit)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var out []model.SupportTicket
	err := q.Find(&out).Error
	return out, err
}

// CountOpen returns how many tickets are not yet resolved (for an admin badge).
func (r *SupportRepository) CountOpen() (int64, error) {
	var n int64
	err := r.db.Model(&model.SupportTicket{}).
		Where("status <> ?", "resolved").Count(&n).Error
	return n, err
}
