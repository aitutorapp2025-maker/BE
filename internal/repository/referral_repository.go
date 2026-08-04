package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// ReferralRepository provides data access for referral attribution records.
type ReferralRepository struct {
	db *gorm.DB
}

// NewReferralRepository builds a ReferralRepository.
func NewReferralRepository(db *gorm.DB) *ReferralRepository {
	return &ReferralRepository{db: db}
}

// FindByStudentCode returns the student who owns the given referral code, or
// ErrNotFound. Codes are stored uppercase; the caller normalises before lookup.
func (r *ReferralRepository) FindStudentByCode(code string) (*model.Student, error) {
	if code == "" {
		return nil, ErrNotFound
	}
	var s model.Student
	err := r.db.Where("referral_code = ?", code).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CodeExists reports whether a referral code is already taken (collision check
// during code generation).
func (r *ReferralRepository) CodeExists(code string) (bool, error) {
	var n int64
	err := r.db.Model(&model.Student{}).Where("referral_code = ?", code).Count(&n).Error
	return n > 0, err
}

// RefereeAlreadyReferred reports whether a student has already been attributed to
// a referrer (a referee can only be counted once).
func (r *ReferralRepository) RefereeAlreadyReferred(refereeID uint) (bool, error) {
	var n int64
	err := r.db.Model(&model.Referral{}).Where("referee_id = ?", refereeID).Count(&n).Error
	return n > 0, err
}

// Create inserts a referral attribution record.
func (r *ReferralRepository) Create(ref *model.Referral) error {
	return r.db.Create(ref).Error
}

// ListRecent returns the most recent referral records for the admin view.
func (r *ReferralRepository) ListRecent(limit int) ([]model.Referral, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []model.Referral
	err := r.db.Order("created_at DESC").Limit(limit).Find(&out).Error
	return out, err
}
