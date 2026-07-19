package model

import "time"

// SchoolClass is a class level (Class 1 … Class 12) in the class master.
type SchoolClass struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:40;not null" json:"name"`   // "Class 1"
	Number    int       `gorm:"not null" json:"number"`         // 1..12
	Active    bool      `gorm:"not null;default:true" json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (SchoolClass) TableName() string { return "classes" }
