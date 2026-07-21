package model

import "time"

// TeachingLanguage is an admin-managed language of instruction (e.g. Tamil,
// English, Hindi, Telugu) offered to students. The mobile/web profile screen
// and the admin student form both show the active languages from this list.
type TeachingLanguage struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:40;uniqueIndex;not null" json:"name"`
	Active    bool      `gorm:"not null;default:true" json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (TeachingLanguage) TableName() string { return "teaching_languages" }
