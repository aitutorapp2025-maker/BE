package model

import "time"

// Student is a learner account managed from the admin panel.
type Student struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:120;not null" json:"name"`
	Phone        string    `gorm:"size:20" json:"phone"`
	ParentPhone  string    `gorm:"size:20" json:"parent_phone"`
	StudentClass string    `gorm:"size:40;not null" json:"student_class"` // e.g. "Class 10"
	Board        string    `gorm:"size:40;not null" json:"board"`         // State Board / CBSE / ICSE
	Medium       string    `gorm:"size:20;not null" json:"medium"`        // English / Tamil
	// StudentGroup is the subject group / stream, asked only for classes that
	// have groups configured (higher secondary — 11 & 12). Blank otherwise.
	StudentGroup string `gorm:"size:60" json:"student_group"` // e.g. "Computer Science"
	// TeachingLanguage is the admin-managed language of instruction (Tamil /
	// English / Hindi / Telugu ...).
	TeachingLanguage string `gorm:"size:40" json:"teaching_language"`
	Plan         string    `gorm:"size:20;not null;default:trial" json:"plan"`       // trial | monthly | yearly
	PayStatus    string    `gorm:"size:20;not null;default:trial" json:"pay_status"` // trial | paid | expired
	JoinedAt     time.Time `json:"joined_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (Student) TableName() string { return "students" }
