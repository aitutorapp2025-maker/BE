package repository

import (
	"context"
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// HomeBannerRepository provides data access for the Home-screen banners. The
// active list is read on every app Home open, so it's cached (no expiry) and
// busted on any create/update/delete.
type HomeBannerRepository struct {
	db    *gorm.DB
	cache *cache.Store
}

// NewHomeBannerRepository builds a HomeBannerRepository. cache may be nil.
func NewHomeBannerRepository(db *gorm.DB, c *cache.Store) *HomeBannerRepository {
	return &HomeBannerRepository{db: db, cache: c}
}

// bust clears the cached active-banners list (call after any write).
func (r *HomeBannerRepository) bust() {
	r.cache.Del(context.Background(), cache.KeyHomeBanners)
}

// List returns all banners (admin), sort order then newest first.
func (r *HomeBannerRepository) List() ([]model.HomeBanner, error) {
	var banners []model.HomeBanner
	err := r.db.Order("sort_order ASC, id DESC").Find(&banners).Error
	return banners, err
}

// ListActive returns only the enabled banners (for the app), cached.
func (r *HomeBannerRepository) ListActive() ([]model.HomeBanner, error) {
	ctx := context.Background()
	var banners []model.HomeBanner
	if r.cache.Get(ctx, cache.KeyHomeBanners, &banners) {
		return banners, nil
	}
	if err := r.db.Where("active = ?", true).
		Order("sort_order ASC, id DESC").Find(&banners).Error; err != nil {
		return nil, err
	}
	r.cache.Set(ctx, cache.KeyHomeBanners, banners)
	return banners, nil
}

// FindByID returns a banner by id, or ErrNotFound.
func (r *HomeBannerRepository) FindByID(id uint) (*model.HomeBanner, error) {
	var b model.HomeBanner
	err := r.db.First(&b, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// Create inserts a new banner.
func (r *HomeBannerRepository) Create(b *model.HomeBanner) error {
	if err := r.db.Create(b).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}

// Update saves changes to a banner. Select("*") writes zero values too, so
// switching a banner inactive or clearing its text/image genuinely persists.
func (r *HomeBannerRepository) Update(b *model.HomeBanner) error {
	if err := r.db.Model(b).Select("*").Omit("created_at").Updates(b).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}

// Delete removes a banner by id.
func (r *HomeBannerRepository) Delete(id uint) error {
	if err := r.db.Delete(&model.HomeBanner{}, id).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}
