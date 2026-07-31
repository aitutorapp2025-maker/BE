package model

import "time"

// CronJob is a background job the admin can enable/disable. The scheduler reads
// this table each tick and runs only the enabled jobs; it records the outcome of
// each run here so the admin can see what happened and when.
type CronJob struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Key         string `gorm:"size:60;uniqueIndex;not null" json:"key"` // stable id, e.g. "trial_reminders"
	Name        string `gorm:"size:120;not null" json:"name"`
	Description string `gorm:"size:300" json:"description"`
	// Schedule: "hourly" (runs every tick) or "daily" (once per calendar day).
	Schedule string `gorm:"size:20;not null;default:daily" json:"schedule"`
	Enabled  bool   `gorm:"not null;default:false" json:"enabled"`

	LastRunAt  *time.Time `json:"last_run_at"`
	LastStatus string     `gorm:"size:20" json:"last_status"` // ok | error
	LastResult string     `gorm:"size:200" json:"last_result"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (CronJob) TableName() string { return "cron_jobs" }
