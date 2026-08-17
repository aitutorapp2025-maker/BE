package repository

import (
	"errors"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// ErrRoleInUse is returned when deleting a role that admins are still assigned to.
var ErrRoleInUse = errors.New("role is assigned to one or more admins")

// AdminRoleRepository provides data access for AdminRole records.
type AdminRoleRepository struct {
	db *gorm.DB
}

// NewAdminRoleRepository builds an AdminRoleRepository.
func NewAdminRoleRepository(db *gorm.DB) *AdminRoleRepository {
	return &AdminRoleRepository{db: db}
}

// List returns every role with its admin usage count.
func (r *AdminRoleRepository) List() ([]model.AdminRole, error) {
	var roles []model.AdminRole
	if err := r.db.Order("name").Find(&roles).Error; err != nil {
		return nil, err
	}
	for i := range roles {
		r.db.Model(&model.Admin{}).Where("role_id = ?", roles[i].ID).
			Count(&roles[i].AdminCount)
	}
	return roles, nil
}

// FindByID returns one role, or ErrNotFound.
func (r *AdminRoleRepository) FindByID(id uint) (*model.AdminRole, error) {
	var role model.AdminRole
	err := r.db.First(&role, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &role, nil
}

// Create inserts a new role.
func (r *AdminRoleRepository) Create(role *model.AdminRole) error {
	return r.db.Create(role).Error
}

// Update saves the role's name/description/permissions. A struct-based
// Updates with Select keeps the json serializer applied to permissions and
// still writes empty values.
func (r *AdminRoleRepository) Update(role *model.AdminRole) error {
	return r.db.Model(&model.AdminRole{ID: role.ID}).
		Select("name", "description", "permissions").
		Updates(role).Error
}

// Delete removes a role; refuses (ErrRoleInUse) while admins still use it.
func (r *AdminRoleRepository) Delete(id uint) error {
	var n int64
	if err := r.db.Model(&model.Admin{}).Where("role_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return ErrRoleInUse
	}
	return r.db.Delete(&model.AdminRole{}, id).Error
}
