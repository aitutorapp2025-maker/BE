package model

import "time"

// ChatConversation is one named AI-tutor chat thread for a student ("New
// chat" in the app). ConvID is the app-generated conversation id; messages
// reference it via ChatMessage.ConvID. Name is auto-derived from the first
// message unless the student renamed it (Named=true), in which case the
// custom name always wins during sync.
type ChatConversation struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	StudentID uint   `gorm:"not null;uniqueIndex:idx_conv_student_cid,priority:1" json:"student_id"`
	ConvID    string `gorm:"size:80;not null;uniqueIndex:idx_conv_student_cid,priority:2" json:"conv_id"`

	Name  string `gorm:"size:80" json:"name"`
	Named bool   `gorm:"not null;default:false" json:"named"` // student renamed it

	LastAt    time.Time `json:"last_at"` // last message time (list ordering)
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (ChatConversation) TableName() string { return "chat_conversations" }
