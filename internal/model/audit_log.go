package model

import "time"

// AuditLog records one authenticated action (an API request) by an admin or a
// student, so an admin can review who did what and when. There are two logical
// streams distinguished by ActorType ("admin" vs "student").
type AuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ActorType  string    `gorm:"size:10;index;not null" json:"actor_type"` // admin | student
	ActorID    uint      `gorm:"index;not null" json:"actor_id"`
	ActorLabel string    `gorm:"size:150" json:"actor_label"` // email (admin) / phone (student)
	Method     string    `gorm:"size:8" json:"method"`
	Path       string    `gorm:"size:200" json:"path"`
	Status     int       `json:"status"`
	IP         string    `gorm:"size:50" json:"ip"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

// TableName sets the table name explicitly.
func (AuditLog) TableName() string { return "audit_logs" }
