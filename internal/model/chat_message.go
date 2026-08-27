package model

import "time"

// ChatMessage is one persisted AI-tutor chat message for a student, so the
// conversation syncs across devices and survives a reinstall. ClientID is the
// app-generated message id; the (StudentID, ClientID) pair is unique so the app
// can re-push its history idempotently without creating duplicates.
type ChatMessage struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	StudentID uint   `gorm:"not null;uniqueIndex:idx_chat_student_client,priority:1" json:"student_id"`
	ClientID  string `gorm:"size:80;not null;uniqueIndex:idx_chat_student_client,priority:2" json:"client_id"`

	// ConvID is the conversation (chat thread) this message belongs to. Legacy
	// rows predate multi-chat and have '' — the app maps them to its "legacy"
	// conversation.
	ConvID string `gorm:"size:80;index" json:"conv_id"`

	Role string `gorm:"size:8;not null" json:"role"`                // "user" | "ai"
	Kind string `gorm:"size:8;not null;default:'text'" json:"kind"` // text | image | pdf
	Text string `gorm:"type:text" json:"text"`

	// HomeworkID links an AI reply to a created homework (0 = none); ImageURL is
	// the server-hosted picture for an image attachment (shown on other devices).
	HomeworkID uint   `gorm:"not null;default:0" json:"homework_id"`
	ImageURL   string `gorm:"size:400" json:"image_url"`

	SentAt    time.Time `json:"sent_at"` // client message time (ordering key)
	CreatedAt time.Time `json:"created_at"`
}
