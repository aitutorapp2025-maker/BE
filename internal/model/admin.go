// Package model holds the GORM data models (database entities).
package model

import "time"

// Admin is a back-office user who signs in to the admin panel to manage
// students, plans, classes and books.
//
// Access control: RoleID nil ⇒ SUPER admin (full access, e.g. the seeded
// first admin). Otherwise the linked AdminRole's permission keys decide which
// side-menu items / settings tabs (and their APIs) the admin can use.
type Admin struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Name         string     `gorm:"size:120;not null" json:"name"`
	Email        string     `gorm:"size:190;uniqueIndex;not null" json:"email"`
	PasswordHash string     `gorm:"size:255;not null" json:"-"` // never serialized
	Role         string     `gorm:"size:40;not null;default:admin" json:"role"`
	RoleID       *uint      `json:"role_id,omitempty"`
	AdminRole    *AdminRole `gorm:"foreignKey:RoleID" json:"admin_role,omitempty"`
	IsActive     bool       `gorm:"not null;default:true" json:"is_active"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	// Resolved access — filled by ApplyRole before serializing (login/me/list),
	// never stored.
	Permissions []string `gorm:"-" json:"permissions"`
	IsSuper     bool     `gorm:"-" json:"is_super"`
}

// TableName sets the table name explicitly.
func (Admin) TableName() string { return "admins" }

// ApplyRole fills the transient Permissions/IsSuper fields from the (preloaded)
// role so API responses carry the resolved access.
func (a *Admin) ApplyRole() {
	if a.RoleID == nil {
		a.IsSuper = true
		a.Permissions = []string{}
		return
	}
	a.IsSuper = false
	if a.AdminRole != nil {
		a.Permissions = a.AdminRole.Permissions
	}
	if a.Permissions == nil {
		a.Permissions = []string{}
	}
}

// HasPerm reports whether the admin may use any of the given permission keys.
// A super admin may use everything; an empty requirement is always allowed.
func (a *Admin) HasPerm(anyOf []string) bool {
	if len(anyOf) == 0 || a.RoleID == nil {
		return true
	}
	var granted []string
	if a.AdminRole != nil {
		granted = a.AdminRole.Permissions
	}
	for _, want := range anyOf {
		for _, g := range granted {
			if g == want {
				return true
			}
		}
	}
	return false
}
