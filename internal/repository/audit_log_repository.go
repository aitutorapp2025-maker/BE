package repository

import (
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/database"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// AuditLogRepository stores and queries the audit trail.
type AuditLogRepository struct {
	db *gorm.DB
}

// NewAuditLogRepository builds an AuditLogRepository.
func NewAuditLogRepository(db *gorm.DB) *AuditLogRepository {
	return &AuditLogRepository{db: db}
}

// Record inserts one audit entry.
func (r *AuditLogRepository) Record(a *model.AuditLog) error {
	return r.db.Create(a).Error
}

// RecordBatch inserts many audit entries in one round trip (used by the buffered
// writer). No-op on an empty slice.
//
// Self-healing across month rollovers: audit_logs is partitioned by month
// (database.MigrateAuditPartitions) and partitions are normally pre-created a
// month ahead, but if the process runs long enough to outlive them (cleanup
// cron disabled, no restarts) an insert would hit a month with no partition.
// That specific failure creates the missing partitions and retries once, so
// audit writes never depend on the cron being enabled.
func (r *AuditLogRepository) RecordBatch(logs []model.AuditLog) error {
	if len(logs) == 0 {
		return nil
	}
	err := r.db.CreateInBatches(logs, 100).Error
	if err != nil && strings.Contains(err.Error(), "no partition of relation") {
		if ensureErr := database.EnsureAuditPartitions(r.db, time.Now()); ensureErr == nil {
			err = r.db.CreateInBatches(logs, 100).Error
		}
	}
	return err
}

// List returns audit entries filtered by actor type (empty = all) and an
// optional time window, newest first, paginated. Returns the page + total count.
func (r *AuditLogRepository) List(actorType string, from, to time.Time, limit, offset int) ([]model.AuditLog, int64, error) {
	q := r.db.Model(&model.AuditLog{})
	if actorType != "" {
		q = q.Where("actor_type = ?", actorType)
	}
	if !from.IsZero() {
		q = q.Where("created_at >= ?", from)
	}
	if !to.IsZero() {
		q = q.Where("created_at <= ?", to)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = 50
	}
	var out []model.AuditLog
	// Omit the potentially large request/response payloads from the list — they're
	// loaded on demand via FindByID (the detail view).
	err := q.Omit("request_body", "response_body").
		Order("created_at DESC").Limit(limit).Offset(offset).Find(&out).Error
	return out, total, err
}

// FindByID returns one audit entry with its full request/response bodies.
func (r *AuditLogRepository) FindByID(id uint) (*model.AuditLog, error) {
	var a model.AuditLog
	if err := r.db.First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}
