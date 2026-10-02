package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// CreditTopupRepository tracks one-time credit purchases (order → payment).
type CreditTopupRepository struct{ db *gorm.DB }

// NewCreditTopupRepository builds a CreditTopupRepository.
func NewCreditTopupRepository(db *gorm.DB) *CreditTopupRepository {
	return &CreditTopupRepository{db: db}
}

// Create inserts a new top-up record (status "created").
func (r *CreditTopupRepository) Create(t *model.CreditTopup) error { return r.db.Create(t).Error }

// FindByOrder returns the top-up for a Razorpay order id, or ErrNotFound.
func (r *CreditTopupRepository) FindByOrder(orderID string) (*model.CreditTopup, error) {
	var t model.CreditTopup
	err := r.db.Where("order_id = ?", orderID).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Update saves changes to a top-up record.
func (r *CreditTopupRepository) Update(t *model.CreditTopup) error { return r.db.Save(t).Error }
