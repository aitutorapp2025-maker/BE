package repository

import (
	"errors"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// CronRepository is the store for admin-managed background jobs.
type CronRepository struct {
	db *gorm.DB
}

// NewCronRepository builds a CronRepository.
func NewCronRepository(db *gorm.DB) *CronRepository {
	return &CronRepository{db: db}
}

// List returns all cron jobs ordered by id.
func (r *CronRepository) List() ([]model.CronJob, error) {
	var jobs []model.CronJob
	err := r.db.Order("id ASC").Find(&jobs).Error
	return jobs, err
}

// FindByKey returns a job by its key, or ErrNotFound.
func (r *CronRepository) FindByKey(key string) (*model.CronJob, error) {
	var j model.CronJob
	err := r.db.Where("key = ?", key).First(&j).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// SetEnabled toggles a job on/off.
func (r *CronRepository) SetEnabled(key string, enabled bool) error {
	res := r.db.Model(&model.CronJob{}).Where("key = ?", key).
		Update("enabled", enabled)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordRun stores the outcome of a run.
func (r *CronRepository) RecordRun(key, status, result string, at time.Time) error {
	return r.db.Model(&model.CronJob{}).Where("key = ?", key).
		Updates(map[string]any{
			"last_run_at": at,
			"last_status": status,
			"last_result": result,
		}).Error
}

// Ensure inserts the job if its key is missing (preserving an existing row's
// enabled flag / metadata). Name/description are refreshed so code changes
// show up; the SCHEDULE is only seeded on create — the admin owns it after
// that (editable from the cron panel), so boot must never overwrite it.
func (r *CronRepository) Ensure(j model.CronJob) error {
	var existing model.CronJob
	err := r.db.Where("key = ?", j.Key).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.Create(&j).Error
	}
	if err != nil {
		return err
	}
	return r.db.Model(&model.CronJob{}).Where("key = ?", j.Key).
		Updates(map[string]any{
			"name":        j.Name,
			"description": j.Description,
		}).Error
}

// SetSchedule updates a job's schedule (admin-edited; validated by the caller).
func (r *CronRepository) SetSchedule(key, schedule string) error {
	res := r.db.Model(&model.CronJob{}).Where("key = ?", key).
		Update("schedule", schedule)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
