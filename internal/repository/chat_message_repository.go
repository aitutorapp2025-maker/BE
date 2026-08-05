package repository

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChatMessageRepository persists the AI-tutor chat so it syncs across devices.
type ChatMessageRepository struct {
	db *gorm.DB
}

// NewChatMessageRepository builds a ChatMessageRepository.
func NewChatMessageRepository(db *gorm.DB) *ChatMessageRepository {
	return &ChatMessageRepository{db: db}
}

// ListByStudent returns a student's full conversation, oldest first.
func (r *ChatMessageRepository) ListByStudent(studentID uint) ([]model.ChatMessage, error) {
	var out []model.ChatMessage
	err := r.db.
		Where("student_id = ?", studentID).
		Order("sent_at ASC, id ASC").
		Find(&out).Error
	return out, err
}

// UpsertBatch inserts messages, ignoring any whose (student_id, client_id)
// already exists — so the app can re-push its history idempotently. No-op on an
// empty slice.
func (r *ChatMessageRepository) UpsertBatch(msgs []model.ChatMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "student_id"}, {Name: "client_id"}},
		DoNothing: true,
	}).CreateInBatches(msgs, 100).Error
}
