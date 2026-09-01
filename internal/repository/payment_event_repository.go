package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// PaymentEventRepository records processed Razorpay charges for webhook
// idempotency.
type PaymentEventRepository struct {
	db *gorm.DB
}

// NewPaymentEventRepository builds a PaymentEventRepository.
func NewPaymentEventRepository(db *gorm.DB) *PaymentEventRepository {
	return &PaymentEventRepository{db: db}
}

// ListForStudent returns a student's processed payment events, newest first —
// the admin's per-student payment history (mandate charge, refunds, renewals).
func (r *PaymentEventRepository) ListForStudent(studentID uint, limit int) ([]model.PaymentEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var out []model.PaymentEvent
	err := r.db.Where("student_id = ?", studentID).
		Order("created_at DESC").Limit(limit).Find(&out).Error
	return out, err
}

// Record inserts a payment event. Returns fresh=false (no error) when the
// payment id was already processed, so the caller can skip a duplicate webhook.
func (r *PaymentEventRepository) Record(paymentID, event string, studentID uint, amountPaise int64) (fresh bool, err error) {
	e := &model.PaymentEvent{
		PaymentID: paymentID, Event: event, StudentID: studentID, AmountPaise: amountPaise,
	}
	err = r.db.Create(e).Error
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return false, nil
	}
	// Some drivers don't map the unique violation to ErrDuplicatedKey — treat an
	// existing row as already-processed.
	var count int64
	if r.db.Model(&model.PaymentEvent{}).Where("payment_id = ?", paymentID).Count(&count); count > 0 {
		return false, nil
	}
	return false, err
}
