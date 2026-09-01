package model

import "time"

// WaMessage is one WhatsApp message on the business number — incoming
// (customer → brand, captured by the Meta webhook) or outgoing (brand →
// customer, recorded when the sender delivers). Powers the admin's
// chat-style WhatsApp inbox.
type WaMessage struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// WaMsgID is Meta's message id (wamid…) — unique so duplicate webhook
	// deliveries never create duplicate rows. Outgoing rows may leave it "".
	WaMsgID   string    `gorm:"size:120;uniqueIndex" json:"wa_msg_id"`
	Phone     string    `gorm:"size:20;index;not null" json:"phone"` // customer, digits intl format
	Name      string    `gorm:"size:120" json:"name"`                // WhatsApp profile name (incoming)
	Direction string    `gorm:"size:8;not null" json:"direction"`    // in | out
	MsgType   string    `gorm:"size:20;not null;default:text" json:"msg_type"`
	Text      string    `gorm:"type:text" json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName sets the table name explicitly.
func (WaMessage) TableName() string { return "wa_messages" }
