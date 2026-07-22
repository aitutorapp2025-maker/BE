package model

import "time"

// ClassGroup is a subject group / stream offered for a class under a board —
// e.g. State Board Class 11 offers "Computer Science", "Biology", "Commerce",
// "Arts" and "Vocational". Higher secondary classes (11 & 12) have them; lower
// classes normally have none, in which case the student is never asked.
//
// A blank Board means the group applies to every board for that class.
type ClassGroup struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	ClassName string `gorm:"size:40;not null;index:idx_class_groups_class_board" json:"class_name"` // "Class 11"
	Board     string `gorm:"size:40;index:idx_class_groups_class_board" json:"board"`               // "State Board" ("" = all boards)
	Name      string `gorm:"size:60;not null" json:"name"`                                          // "Computer Science"
	SortOrder int    `gorm:"not null;default:0" json:"sort_order"`
	// No `default:true` here on purpose — GORM substitutes a column default for
	// a zero-valued field on INSERT, which would silently flip a group created
	// as inactive back to active. Handlers always set Active explicitly.
	Active bool `gorm:"not null" json:"active"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (ClassGroup) TableName() string { return "class_groups" }
