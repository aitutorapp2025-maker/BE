package model

import "time"

// Notification is one delivered push, persisted so the mobile/web app can show a
// notifications feed with an unread count (FCM alone is fire-and-forget). A row
// with StudentID = 0 is a broadcast to ALL customers; a non-zero StudentID is
// targeted to that student. The per-student "read" state is tracked on the
// device (last-read id), so there's no read flag here.
type Notification struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	// Covered by the composite idx_notifications_student_created — no standalone
	// index. (created_at keeps its own index for the global broadcast feed.)
	StudentID uint      `gorm:"not null" json:"student_id"` // 0 = all customers
	Title     string    `gorm:"size:200" json:"title"`
	Body      string    `gorm:"size:1000" json:"body"`
	Image     string    `gorm:"size:400" json:"image"`
	Type      string    `gorm:"size:30" json:"type"` // announcement | reminder | …
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// TableName sets the table name explicitly.
func (Notification) TableName() string { return "notifications" }
