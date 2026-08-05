package repository

import (
	"context"
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// PlanRepository provides data access for Plan records. The full plan list is
// read on every landing/app open, so List() is cached (no expiry) and busted on
// any create/update/delete.
type PlanRepository struct {
	db    *gorm.DB
	cache *cache.Store
}

// NewPlanRepository builds a PlanRepository. cache may be nil (caching off).
func NewPlanRepository(db *gorm.DB, c *cache.Store) *PlanRepository {
	return &PlanRepository{db: db, cache: c}
}

// bust clears the cached plan list (call after any write).
func (r *PlanRepository) bust() { r.cache.Del(context.Background(), cache.KeyPlans) }

// List returns all plans ordered by price (cached).
func (r *PlanRepository) List() ([]model.Plan, error) {
	ctx := context.Background()
	var plans []model.Plan
	if r.cache.Get(ctx, cache.KeyPlans, &plans) {
		return plans, nil
	}
	if err := r.db.Order("price_rupees ASC").Find(&plans).Error; err != nil {
		return nil, err
	}
	r.cache.Set(ctx, cache.KeyPlans, plans)
	return plans, nil
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
	if err := r.db.Create(p).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}

// Update saves changes to an existing plan.
func (r *PlanRepository) Update(p *model.Plan) error {
	if err := r.db.Save(p).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}

// Delete removes a plan by id.
func (r *PlanRepository) Delete(id uint) error {
	if err := r.db.Delete(&model.Plan{}, id).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}
