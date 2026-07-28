package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// PlanRepository provides data access for Plan records.
type PlanRepository struct {
	db *gorm.DB
}

// NewPlanRepository builds a PlanRepository.
func NewPlanRepository(db *gorm.DB) *PlanRepository {
	return &PlanRepository{db: db}
}

// List returns all plans ordered by price.
func (r *PlanRepository) List() ([]model.Plan, error) {
	var plans []model.Plan
	err := r.db.Order("price_rupees ASC").Find(&plans).Error
	return plans, err
}

// FindByID returns a plan by id, or ErrNotFound.
func (r *PlanRepository) FindByID(id uint) (*model.Plan, error) {
	var p model.Plan
	err := r.db.First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FindTrial returns the free-trial plan (the one flagged is_trial). Used to
// give new students their trial on onboarding.
func (r *PlanRepository) FindTrial() (*model.Plan, error) {
	var p model.Plan
	err := r.db.Where("is_trial = ?", true).Order("id ASC").First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FindByRazorpayPlanID returns the plan linked to a Razorpay plan id (used by
// the webhook to know how many credits a charge grants).
func (r *PlanRepository) FindByRazorpayPlanID(planID string) (*model.Plan, error) {
	var p model.Plan
	err := r.db.Where("razorpay_plan_id = ?", planID).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Create inserts a new plan.
func (r *PlanRepository) Create(p *model.Plan) error {
	return r.db.Create(p).Error
}

// Update saves changes to an existing plan.
func (r *PlanRepository) Update(p *model.Plan) error {
	return r.db.Save(p).Error
}

// Delete removes a plan by id.
func (r *PlanRepository) Delete(id uint) error {
	return r.db.Delete(&model.Plan{}, id).Error
}
