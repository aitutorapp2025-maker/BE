package model

import "time"

// AuditLog records one authenticated action (an API request) by an admin or a
// student, so an admin can review who did what and when. There are two logical
// streams distinguished by ActorType ("admin" vs "student").
type AuditLog struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	// ActorType is covered by the composite idx_audit_actor_created — no standalone
	// index. ActorID keeps its own (not a leading column of any composite).
	ActorType  string `gorm:"size:10;not null" json:"actor_type"` // admin | student
	ActorID    uint   `gorm:"index;not null" json:"actor_id"`
	ActorLabel string `gorm:"size:150" json:"actor_label"` // email (admin) / phone (student)
	Method     string `gorm:"size:8" json:"method"`
	Path       string `gorm:"size:200" json:"path"`
	Query      string `gorm:"size:400" json:"query"` // raw query string (?a=b)
	Status     int    `json:"status"`
	IP         string `gorm:"size:50" json:"ip"`
	UserAgent  string `gorm:"size:300" json:"user_agent"`
	LatencyMs  int64  `json:"latency_ms"` // handler time in milliseconds
	// RequestBody / ResponseBody are the decrypted request and plaintext response
	// payloads, captured for admin review. Secrets (password/token/…) are redacted
	// and each is capped in size; binary/multipart bodies are omitted. Excluded
	// from the list query (loaded only via the detail endpoint).
	RequestBody  string    `gorm:"type:text" json:"request_body"`
	ResponseBody string    `gorm:"type:text" json:"response_body"`
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
}

// TableName sets the table name explicitly.
func (AuditLog) TableName() string { return "audit_logs" }
