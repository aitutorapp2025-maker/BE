package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// CreditPackRepository provides data access for admin-managed credit top-up packs.
type CreditPackRepository struct{ db *gorm.DB }

// NewCreditPackRepository builds a CreditPackRepository.
func NewCreditPackRepository(db *gorm.DB) *CreditPackRepository {
	return &CreditPackRepository{db: db}
}

// List returns packs ordered for display; activeOnly restricts to enabled packs.
func (r *CreditPackRepository) List(activeOnly bool) ([]model.CreditPack, error) {
	var packs []model.CreditPack
	q := r.db.Order("sort ASC, amount_rupees ASC")
	if activeOnly {
		q = q.Where("active = ?", true)
	}
	return packs, q.Find(&packs).Error
}

// FindByID returns a pack by id, or ErrNotFound.
func (r *CreditPackRepository) FindByID(id uint) (*model.CreditPack, error) {
	var p model.CreditPack
	err := r.db.First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Create inserts a new pack.
func (r *CreditPackRepository) Create(p *model.CreditPack) error { return r.db.Create(p).Error }

// Update saves changes to a pack.
func (r *CreditPackRepository) Update(p *model.CreditPack) error { return r.db.Save(p).Error }

// Delete removes a pack.
func (r *CreditPackRepository) Delete(id uint) error {
	return r.db.Delete(&model.CreditPack{}, id).Error
}

// Count returns how many packs exist (used to seed defaults once).
func (r *CreditPackRepository) Count() (int64, error) {
	var n int64
	return n, r.db.Model(&model.CreditPack{}).Count(&n).Error
}
