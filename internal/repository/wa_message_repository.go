package repository

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WaMessageRepository stores the WhatsApp inbox (incoming webhook messages +
// outgoing sends) for the admin chat view.
type WaMessageRepository struct {
	db *gorm.DB
}

// NewWaMessageRepository builds a WaMessageRepository.
func NewWaMessageRepository(db *gorm.DB) *WaMessageRepository {
	return &WaMessageRepository{db: db}
}

// Save inserts a message; duplicate wa_msg_id rows (webhook redelivery) are
// silently ignored.
func (r *WaMessageRepository) Save(m *model.WaMessage) error {
	if strings.TrimSpace(m.WaMsgID) == "" {
		return r.db.Create(m).Error
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "wa_msg_id"}},
		DoNothing: true,
	}).Create(m).Error
}

// WaConversation is one chat thread summary for the inbox list.
type WaConversation struct {
	Phone    string `json:"phone"`
	Name     string `json:"name"`
	LastText string `json:"last_text"`
	LastDir  string `json:"last_dir"`
	LastAt   string `json:"last_at"`
	Count    int64  `json:"count"`
}

// Conversations returns thread summaries, most recent first.
func (r *WaMessageRepository) Conversations(limit int) ([]WaConversation, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var out []WaConversation
	err := r.db.Raw(`
		SELECT m.phone,
		       COALESCE(NULLIF((SELECT name FROM wa_messages n
		                        WHERE n.phone = m.phone AND n.name <> ''
		                        ORDER BY n.created_at DESC LIMIT 1), ''), '') AS name,
		       (SELECT text FROM wa_messages t WHERE t.phone = m.phone
		        ORDER BY t.created_at DESC LIMIT 1) AS last_text,
		       (SELECT direction FROM wa_messages d WHERE d.phone = m.phone
		        ORDER BY d.created_at DESC LIMIT 1) AS last_dir,
		       MAX(m.created_at)::text AS last_at,
		       COUNT(*) AS count
		FROM wa_messages m
		GROUP BY m.phone
		ORDER BY MAX(m.created_at) DESC
		LIMIT ?`, limit).Scan(&out).Error
	return out, err
}

// Thread returns one phone's messages, oldest first (chat order).
func (r *WaMessageRepository) Thread(phone string, limit int) ([]model.WaMessage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var out []model.WaMessage
	err := r.db.Where("phone = ?", phone).
		Order("created_at ASC").Limit(limit).Find(&out).Error
	return out, err
}
