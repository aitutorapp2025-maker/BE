package model

import "time"

// DeviceToken is an FCM push token for a single device. It is registered when
// the app first opens (before login, so StudentID/Phone are empty), then mapped
// to a student once they sign in with their mobile number. A student may have
// several devices; each device (token) maps to at most one student.
type DeviceToken struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Token     string    `gorm:"size:255;uniqueIndex;not null" json:"token"`
	Platform  string    `gorm:"size:20" json:"platform"` // android | ios | web
	StudentID *uint     `gorm:"index" json:"student_id"` // nil until mapped to a student
	Phone     string    `gorm:"size:20;index" json:"phone"` // the mapped mobile number
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (DeviceToken) TableName() string { return "device_tokens" }
