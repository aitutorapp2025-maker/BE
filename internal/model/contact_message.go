package model

import "time"

// ContactMessage is a "Get in touch" submission from the public landing page.
type ContactMessage struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:120;not null" json:"name"`
	Email     string    `gorm:"size:190" json:"email"` // email or phone the visitor entered
	Message   string    `gorm:"size:2000;not null" json:"message"`
	Status    string    `gorm:"size:20;not null;default:new" json:"status"` // new | read
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (ContactMessage) TableName() string { return "contact_messages" }
