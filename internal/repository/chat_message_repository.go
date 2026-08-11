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

// maxChatHistory caps how many recent messages the server returns per student.
// A conversation grows without bound over months, and every row is loaded and
// E2E-encrypted on each chat-screen open — so we return only the most recent
// window. Nothing is lost: the app keeps its own local history and re-pushes it
// idempotently (UpsertBatch), so older messages stay on the device and in the DB.
const maxChatHistory = 200

// ListByStudent returns the student's most recent messages (up to
// maxChatHistory), oldest first for display. The DB fetch is newest-first with a
// LIMIT (served by idx_chat_student_sent), then reversed in memory.
func (r *ChatMessageRepository) ListByStudent(studentID uint) ([]model.ChatMessage, error) {
	var out []model.ChatMessage
	err := r.db.
		Where("student_id = ?", studentID).
		Order("sent_at DESC, id DESC").
		Limit(maxChatHistory).
		Find(&out).Error
	if err != nil {
		return nil, err
	}
	// Reverse to oldest-first so the app renders the window in reading order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
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
