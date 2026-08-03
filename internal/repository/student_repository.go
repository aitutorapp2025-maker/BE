package repository

import (
	"errors"
	"time"

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

// FindByPhone returns a student by phone number, or ErrNotFound.
func (r *StudentRepository) FindByPhone(phone string) (*model.Student, error) {
	var s model.Student
	err := r.db.Where("phone = ?", phone).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ExpireOverdueTrials marks trials as expired once their trial window has passed
// AND the student never enabled autopay (no active mandate). Students who did
// enable autopay are left alone — Razorpay auto-debits the base plan at start_at
// and the charge webhook converts them to paid. Returns how many were expired.
func (r *StudentRepository) ExpireOverdueTrials(now time.Time) (int64, error) {
	res := r.db.Model(&model.Student{}).
		Where("pay_status = ?", "trial").
		Where("autopay_active = ?", false).
		Where("trial_ends_at IS NOT NULL AND trial_ends_at < ?", now).
		Update("pay_status", "expired")
	return res.RowsAffected, res.Error
}

// TrialsEndingWithin returns students still on trial whose trial ends between
// now and now+days — the source for the trial-reminder cron.
func (r *StudentRepository) TrialsEndingWithin(now time.Time, days int) ([]model.Student, error) {
	end := now.AddDate(0, 0, days)
	var out []model.Student
	err := r.db.Where("pay_status = ?", "trial").
		Where("trial_ends_at IS NOT NULL AND trial_ends_at >= ? AND trial_ends_at <= ?", now, end).
		Find(&out).Error
	return out, err
}

// FindBySubscriptionID returns the student with the given Razorpay subscription
// id (used by the payment webhook to match a charge back to a student).
func (r *StudentRepository) FindBySubscriptionID(subID string) (*model.Student, error) {
	var s model.Student
	err := r.db.Where("razorpay_subscription_id = ?", subID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// FindByCustomerID returns the student with the given Razorpay customer id
// (used by the headless UPI-mandate webhook to match a payment to a student).
func (r *StudentRepository) FindByCustomerID(customerID string) (*model.Student, error) {
	if customerID == "" {
		return nil, ErrNotFound
	}
	var s model.Student
	err := r.db.Where("razorpay_customer_id = ?", customerID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// ChargeableMandates returns students whose UPI-AutoPay mandate is due for a
// recurring debit: autopay active, a stored token, and NextChargeAt in the past.
func (r *StudentRepository) ChargeableMandates(now time.Time, limit int) ([]model.Student, error) {
	var out []model.Student
	err := r.db.Where("autopay_active = ? AND razorpay_token_id <> ''", true).
		Where("next_charge_at IS NOT NULL AND next_charge_at <= ?", now).
		Limit(limit).Find(&out).Error
	return out, err
}

// FindByGoogleID returns the student with the given Google account id.
func (r *StudentRepository) FindByGoogleID(googleID string) (*model.Student, error) {
	var s model.Student
	err := r.db.Where("google_id = ?", googleID).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// FindByEmail returns the student with the given email (used to link an
// existing account to Google on first SSO sign-in).
func (r *StudentRepository) FindByEmail(email string) (*model.Student, error) {
	if email == "" {
		return nil, ErrNotFound
	}
	var s model.Student
	err := r.db.Where("email = ?", email).First(&s).Error
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
