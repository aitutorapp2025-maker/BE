package repository

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FirebaseStatsRepository stores the daily analytics + crash aggregates synced
// from the Firebase BigQuery export.
type FirebaseStatsRepository struct {
	db *gorm.DB
}

// NewFirebaseStatsRepository builds a FirebaseStatsRepository.
func NewFirebaseStatsRepository(db *gorm.DB) *FirebaseStatsRepository {
	return &FirebaseStatsRepository{db: db}
}

// UpsertAnalytics inserts or replaces a day's analytics aggregate.
func (r *FirebaseStatsRepository) UpsertAnalytics(row *model.AnalyticsDaily) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "day"}},
		UpdateAll: true,
	}).Create(row).Error
}

// UpsertCrash inserts or replaces a day's crash aggregate.
func (r *FirebaseStatsRepository) UpsertCrash(row *model.CrashDaily) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "day"}},
		UpdateAll: true,
	}).Create(row).Error
}

// AnalyticsForDay returns one day's analytics row (or gorm.ErrRecordNotFound).
func (r *FirebaseStatsRepository) AnalyticsForDay(day string) (*model.AnalyticsDaily, error) {
	var row model.AnalyticsDaily
	err := r.db.Where("day = ?", day).First(&row).Error
	return &row, err
}

// ListAnalytics returns the most recent `days` analytics rows, oldest first.
func (r *FirebaseStatsRepository) ListAnalytics(days int) ([]model.AnalyticsDaily, error) {
	if days <= 0 {
		days = 30
	}
	var rows []model.AnalyticsDaily
	err := r.db.Order("day DESC").Limit(days).Find(&rows).Error
	reverse(rows)
	return rows, err
}

// ListCrash returns the most recent `days` crash rows, oldest first.
func (r *FirebaseStatsRepository) ListCrash(days int) ([]model.CrashDaily, error) {
	if days <= 0 {
		days = 30
	}
	var rows []model.CrashDaily
	err := r.db.Order("day DESC").Limit(days).Find(&rows).Error
	reverseCrash(rows)
	return rows, err
}

func reverse(rows []model.AnalyticsDaily) {
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
}

func reverseCrash(rows []model.CrashDaily) {
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
}
