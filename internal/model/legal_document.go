package model

import "time"

// LegalDocument is an admin-editable legal page (Terms & Conditions, Privacy
// Policy, Refund Policy) shown in the app. Content is plain text where lines
// beginning with "# " are rendered as section headings.
type LegalDocument struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Key       string    `gorm:"size:40;uniqueIndex;not null" json:"key"` // terms | privacy | refund
	Title     string    `gorm:"size:160;not null" json:"title"`
	Content   string    `gorm:"type:text" json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (LegalDocument) TableName() string { return "legal_documents" }
