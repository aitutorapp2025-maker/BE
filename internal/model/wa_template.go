package model

import "time"

// WaTemplate is a local mirror of a WhatsApp message template on the WABA. It is
// kept in sync with Meta (see the Sync endpoint) so the admin list is fast, the
// Meta template id is cached for edit/delete-by-id, and templates can be linked
// to campaigns. Meta remains the source of truth for approval status.
type WaTemplate struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// MetaID is Meta's template id, required to edit a template (POST /{id}).
	MetaID   string `gorm:"size:120;index" json:"meta_id"`
	Name     string `gorm:"size:120;not null;uniqueIndex:idx_wa_tpl_name_lang" json:"name"`
	Language string `gorm:"size:16;not null;uniqueIndex:idx_wa_tpl_name_lang" json:"language"`
	Category string `gorm:"size:32" json:"category"`                 // MARKETING | UTILITY | AUTHENTICATION
	Status   string `gorm:"size:24;index" json:"status"`             // APPROVED | PENDING | REJECTED | PAUSED
	HeaderFormat string `gorm:"size:16" json:"header_format"`        // IMAGE | TEXT | VIDEO | DOCUMENT | ""
	BodyText     string `gorm:"type:text" json:"body_text"`
	Footer       string `gorm:"size:160" json:"footer"`
	BodyParams   int    `gorm:"default:0" json:"body_params"` // count of {{n}} in the body
	RejectedReason string `gorm:"size:255" json:"rejected_reason"` // why Meta rejected (status REJECTED)
	Buttons        string `gorm:"type:text" json:"buttons"`        // JSON array of buttons (type/text/url/phone_number)
	ImageData      string `gorm:"type:text" json:"-"`               // base64 header image, kept for auto-reuse in campaigns (never sent to the client)
	HasImage       bool   `gorm:"-" json:"has_image"`              // computed: a stored header image exists
	LastSyncedAt time.Time `json:"last_synced_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (WaTemplate) TableName() string { return "wa_templates" }
