package repository

import (
	"context"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// SettingRepository provides access to the singleton settings row (id = 1).
//
// The settings row is read on almost every request (each provider — Razorpay,
// Google SSO, autopay, AI, FCM — resolves its config from it per call), so it's
// cached in Redis with NO expiry and rebuilt whenever Save writes a change.
type SettingRepository struct {
	db    *gorm.DB
	cache *cache.Store
}

// NewSettingRepository builds a SettingRepository. cache may be nil (caching off).
func NewSettingRepository(db *gorm.DB, c *cache.Store) *SettingRepository {
	return &SettingRepository{db: db, cache: c}
}

// Get returns the settings row, creating a default one if none exists. Served
// from the Redis cache when warm; a miss loads from Postgres and re-warms it.
// Each call decodes into a fresh value, so callers can safely mutate + Save.
func (r *SettingRepository) Get() (*model.Setting, error) {
	ctx := context.Background()
	var s model.Setting
	if r.cache.Get(ctx, cache.KeySettings, &s) {
		return &s, nil
	}
	err := r.db.First(&s, 1).Error
	if err == gorm.ErrRecordNotFound {
		s = model.Setting{ID: 1, AppName: "Vaha AI", EmailNotifications: true}
		if err := r.db.Create(&s).Error; err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	r.cache.Set(ctx, cache.KeySettings, s)
	return &s, nil
}

// Save persists the settings row and busts the cache so the next Get rebuilds it.
func (r *SettingRepository) Save(s *model.Setting) error {
	s.ID = 1
	if err := r.db.Save(s).Error; err != nil {
		return err
	}
	r.cache.Del(context.Background(), cache.KeySettings)
	return nil
}

// SetSyncStatus records the outcome of the last BigQuery sync (targeted update
// so it never clobbers other settings), then busts the settings cache.
func (r *SettingRepository) SetSyncStatus(msg string) error {
	now := time.Now()
	if err := r.db.Model(&model.Setting{}).Where("id = ?", 1).
		Updates(map[string]any{
			"firebase_sync_status": msg,
			"firebase_sync_at":     now,
		}).Error; err != nil {
		return err
	}
	r.cache.Del(context.Background(), cache.KeySettings)
	return nil
}
