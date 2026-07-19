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
	err := r.db.Where("email = ?", email).First(&admin).Error
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
	err := r.db.First(&admin, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &admin, nil
}

// Create inserts a new admin.
func (r *AdminRepository) Create(admin *model.Admin) error {
	return r.db.Create(admin).Error
}

// Count returns the total number of admin records.
func (r *AdminRepository) Count() (int64, error) {
	var n int64
	err := r.db.Model(&model.Admin{}).Count(&n).Error
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
