package model

import "time"

// WaCampaign is one WhatsApp broadcast run: an approved template sent to many
// recipients (uploaded via Excel). Counts are updated as the worker delivers.
type WaCampaign struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Name          string    `gorm:"size:160" json:"name"`
	TemplateName  string    `gorm:"size:120;not null" json:"template_name"`
	TemplateLang  string    `gorm:"size:16" json:"template_lang"`
	HeaderImageID string    `gorm:"size:200" json:"header_image_id"` // Meta media id (optional)
	Status        string    `gorm:"size:16;default:draft" json:"status"` // draft | scheduled | sending | done
	Total         int       `json:"total"`
	Sent          int       `json:"sent"`
	Failed        int       `json:"failed"`
	// ScheduledAt, when set and in the future, defers the send: the campaign
	// stays "scheduled" until the dispatcher cron enqueues it at that time.
	ScheduledAt   *time.Time `gorm:"index" json:"scheduled_at"`
	// ImageData holds the base64 header image for scheduled campaigns so a fresh
	// Meta media id can be uploaded at dispatch time (media ids expire ~30 days).
	ImageData     string    `gorm:"type:text" json:"-"`
	CreatedBy     uint      `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (WaCampaign) TableName() string { return "wa_campaigns" }

// WaCampaignRecipient is one person in a campaign, with their per-row template
// body parameters and delivery status.
type WaCampaignRecipient struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	CampaignID uint       `gorm:"index;not null" json:"campaign_id"`
	Phone      string     `gorm:"size:24;not null" json:"phone"`
	Params     string     `gorm:"type:text" json:"params"` // JSON array of body params for {{1}},{{2}}…
	Status     string     `gorm:"size:12;index;default:queued" json:"status"` // queued | sent | failed
	WaMsgID    string     `gorm:"size:120" json:"wa_msg_id"`
	Error      string     `gorm:"size:400" json:"error"`
	SentAt     *time.Time `json:"sent_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// TableName sets the table name explicitly.
func (WaCampaignRecipient) TableName() string { return "wa_campaign_recipients" }
