package model

import "time"

// SupportTicket is a "Report a problem" ticket raised by a student from Help &
// Support. The student tracks its status + the admin reply; the admin sees all
// tickets and responds.
type SupportTicket struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	StudentID    uint   `gorm:"index;not null" json:"student_id"`
	StudentLabel string `gorm:"size:120" json:"student_label"` // phone/name for the admin list

	Message       string `gorm:"type:text;not null" json:"message"`
	AttachmentURL string `gorm:"size:400" json:"attachment_url"` // optional uploaded file (signed /media URL)

	// Status: open | in_progress | resolved. AdminReply is shown to the student.
	Status     string     `gorm:"size:20;not null;default:'open'" json:"status"`
	AdminReply string     `gorm:"type:text" json:"admin_reply"`
	RepliedAt  *time.Time `json:"replied_at"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
