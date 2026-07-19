// Package model holds the GORM data models (database entities).
package model

import "time"

// Admin is a back-office user who signs in to the admin panel to manage
// students, plans, classes and books.
type Admin struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Name         string     `gorm:"size:120;not null" json:"name"`
	Email        string     `gorm:"size:190;uniqueIndex;not null" json:"email"`
	PasswordHash string     `gorm:"size:255;not null" json:"-"` // never serialized
	Role         string     `gorm:"size:40;not null;default:admin" json:"role"`
	IsActive     bool       `gorm:"not null;default:true" json:"is_active"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (Admin) TableName() string { return "admins" }
