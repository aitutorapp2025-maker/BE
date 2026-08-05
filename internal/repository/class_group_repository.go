package repository

import (
	"context"
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// ClassGroupRepository provides data access for the class-group master
// (subject groups / streams offered for a class + board, e.g. Class 11
// State Board → Computer Science / Biology / Commerce / Arts / Vocational).
//
// The active list (per class + board) is read on the app profile screen, so
// it's cached (no expiry) under a per-(class,board) key and every write busts
// the whole class-group key space.
type ClassGroupRepository struct {
	db    *gorm.DB
	cache *cache.Store
}

// NewClassGroupRepository builds a ClassGroupRepository. cache may be nil.
func NewClassGroupRepository(db *gorm.DB, c *cache.Store) *ClassGroupRepository {
	return &ClassGroupRepository{db: db, cache: c}
}

// bust clears every cached active-group list (call after any write — a single
// group can affect several class/board combinations).
func (r *ClassGroupRepository) bust() {
	r.cache.DelPattern(context.Background(), cache.KeyClassGroupsPrefix+"*")
}

// ordered applies the standard display order: class, then board, then the
// admin-defined sort order, then id for stability.
func (r *ClassGroupRepository) ordered() *gorm.DB {
	return r.db.Order("class_name ASC").Order("board ASC").
		Order("sort_order ASC").Order("id ASC")
}

// List returns every group (admin view), including inactive ones.
func (r *ClassGroupRepository) List() ([]model.ClassGroup, error) {
	var groups []model.ClassGroup
	err := r.ordered().Find(&groups).Error
	return groups, err
}

// ListByClass returns every group for one class (admin view, all boards).
func (r *ClassGroupRepository) ListByClass(className string) ([]model.ClassGroup, error) {
	var groups []model.ClassGroup
	err := r.ordered().Where("class_name = ?", className).Find(&groups).Error
	return groups, err
}

// ListActive returns the enabled groups a student may pick for a class +
// board. Groups with a blank board apply to every board, so they're included
// alongside the board-specific ones. Cached per (class, board).
func (r *ClassGroupRepository) ListActive(className, board string) ([]model.ClassGroup, error) {
	ctx := context.Background()
	key := cache.KeyClassGroupsPrefix + className + "|" + board
	var groups []model.ClassGroup
	if r.cache.Get(ctx, key, &groups) {
		return groups, nil
	}
	q := r.ordered().Where("active = ?", true)
	if className != "" {
		q = q.Where("class_name = ?", className)
	}
	if board != "" {
		q = q.Where("board = ? OR board = ''", board)
	}
	if err := q.Find(&groups).Error; err != nil {
		return nil, err
	}
	r.cache.Set(ctx, key, groups)
	return groups, nil
}

// FindByID returns a group by id, or ErrNotFound.
func (r *ClassGroupRepository) FindByID(id uint) (*model.ClassGroup, error) {
	var g model.ClassGroup
	err := r.db.First(&g, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// Create inserts a new group.
func (r *ClassGroupRepository) Create(g *model.ClassGroup) error {
	if err := r.db.Create(g).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}

// Update saves changes to a group.
func (r *ClassGroupRepository) Update(g *model.ClassGroup) error {
	if err := r.db.Save(g).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}

// Delete removes a group by id.
func (r *ClassGroupRepository) Delete(id uint) error {
	if err := r.db.Delete(&model.ClassGroup{}, id).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}

// RenameClass repoints every group of a class when the class is renamed, so
// the groups stay attached to it.
func (r *ClassGroupRepository) RenameClass(oldName, newName string) error {
	if oldName == "" || oldName == newName {
		return nil
	}
	if err := r.db.Model(&model.ClassGroup{}).
		Where("class_name = ?", oldName).
		Update("class_name", newName).Error; err != nil {
		return err
	}
	r.bust()
	return nil
}
