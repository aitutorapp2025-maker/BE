// Package repository holds the data-access layer (GORM queries) for each model.
package repository

import (
	"errors"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// ErrNotFound is returned when a record does not exist.
var ErrNotFound = errors.New("record not found")

// AdminRepository provides data access for Admin records.
type AdminRepository struct {
	db *gorm.DB
}

// NewAdminRepository builds an AdminRepository.
func NewAdminRepository(db *gorm.DB) *AdminRepository {
	return &AdminRepository{db: db}
}

// FindByEmail returns the admin with the given email, or ErrNotFound.
func (r *AdminRepository) FindByEmail(email string) (*model.Admin, error) {
	var admin model.Admin
	err := r.db.Preload("AdminRole").Where("email = ?", email).First(&admin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

// FindByID returns the admin with the given id, or ErrNotFound.
func (r *AdminRepository) FindByID(id uint) (*model.Admin, error) {
	var admin model.Admin
	err := r.db.Preload("AdminRole").First(&admin, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

// List returns every admin (role preloaded), newest first.
func (r *AdminRepository) List() ([]model.Admin, error) {
	var admins []model.Admin
	err := r.db.Preload("AdminRole").Order("id").Find(&admins).Error
	return admins, err
}

// Create inserts a new admin.
func (r *AdminRepository) Create(admin *model.Admin) error {
	return r.db.Create(admin).Error
}

// Update saves name/email/role/active on an existing admin. Select lists the
// exact columns so a nil RoleID genuinely clears the role (super admin).
func (r *AdminRepository) Update(admin *model.Admin) error {
	return r.db.Model(&model.Admin{}).Where("id = ?", admin.ID).
		Select("name", "email", "role_id", "is_active").
		Updates(map[string]any{
			"name":      admin.Name,
			"email":     admin.Email,
			"role_id":   admin.RoleID,
			"is_active": admin.IsActive,
		}).Error
}

// Delete removes an admin permanently.
func (r *AdminRepository) Delete(id uint) error {
	return r.db.Delete(&model.Admin{}, id).Error
}

// Count returns the total number of admin records.
func (r *AdminRepository) Count() (int64, error) {
	var n int64
	err := r.db.Model(&model.Admin{}).Count(&n).Error
	return n, err
}

// CountSupers returns how many ACTIVE super admins (no role) exist — used to
// refuse deleting/demoting the last one.
func (r *AdminRepository) CountSupers() (int64, error) {
	var n int64
	err := r.db.Model(&model.Admin{}).
		Where("role_id IS NULL AND is_active = ?", true).Count(&n).Error
	return n, err
}

// TouchLastLogin updates the admin's last_login_at to now.
func (r *AdminRepository) TouchLastLogin(id uint) error {
	now := time.Now()
	return r.db.Model(&model.Admin{}).Where("id = ?", id).
		Update("last_login_at", now).Error
}

// UpdatePassword sets a new password hash for the admin.
func (r *AdminRepository) UpdatePassword(id uint, hash string) error {
	return r.db.Model(&model.Admin{}).Where("id = ?", id).
		Update("password_hash", hash).Error
}
