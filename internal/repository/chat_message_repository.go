package repository

import (
	"errors"

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
// convID scopes to one conversation: "" = all (old app builds), "legacy" also
// matches pre-multi-chat rows whose conv_id is ''.
func (r *ChatMessageRepository) ListByStudent(studentID uint, convID string) ([]model.ChatMessage, error) {
	q := r.db.Where("student_id = ?", studentID)
	switch convID {
	case "":
		// all conversations (backward-compatible full history)
	case "legacy":
		q = q.Where("conv_id IN ('', 'legacy')")
	default:
		q = q.Where("conv_id = ?", convID)
	}
	var out []model.ChatMessage
	err := q.
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

// ListConversations returns the student's chat threads, most recent first.
func (r *ChatMessageRepository) ListConversations(studentID uint) ([]model.ChatConversation, error) {
	var out []model.ChatConversation
	err := r.db.
		Where("student_id = ?", studentID).
		Order("last_at DESC, id DESC").
		Limit(100).
		Find(&out).Error
	return out, err
}

// UpsertConversations merges the app's conversation registry: new threads are
// inserted; existing ones take the incoming name/last_at — EXCEPT that a name
// the student set by hand (Named=true) is never overwritten by an auto-name.
func (r *ChatMessageRepository) UpsertConversations(studentID uint, convs []model.ChatConversation) error {
	for _, in := range convs {
		if in.ConvID == "" {
			continue
		}
		var ex model.ChatConversation
		err := r.db.Where("student_id = ? AND conv_id = ?", studentID, in.ConvID).First(&ex).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			in.ID = 0
			in.StudentID = studentID
			if cerr := r.db.Create(&in).Error; cerr != nil {
				return cerr
			}
			continue
		}
		if err != nil {
			return err
		}
		updates := map[string]any{}
		if in.LastAt.After(ex.LastAt) {
			updates["last_at"] = in.LastAt
		}
		if in.Named || !ex.Named {
			if in.Name != "" && in.Name != ex.Name {
				updates["name"] = in.Name
			}
			if in.Named && !ex.Named {
				updates["named"] = true
			}
		}
		if len(updates) > 0 {
			if uerr := r.db.Model(&model.ChatConversation{}).
				Where("id = ?", ex.ID).Updates(updates).Error; uerr != nil {
				return uerr
			}
		}
	}
	return nil
}

// DeleteConversation removes one thread and all its messages (student-scoped).
// Deleting "legacy" also clears pre-multi-chat rows whose conv_id is ''.
func (r *ChatMessageRepository) DeleteConversation(studentID uint, convID string) error {
	if convID == "" {
		return nil
	}
	msgQ := r.db.Where("student_id = ?", studentID)
	if convID == "legacy" {
		msgQ = msgQ.Where("conv_id IN ('', 'legacy')")
	} else {
		msgQ = msgQ.Where("conv_id = ?", convID)
	}
	if err := msgQ.Delete(&model.ChatMessage{}).Error; err != nil {
		return err
	}
	return r.db.
		Where("student_id = ? AND conv_id = ?", studentID, convID).
		Delete(&model.ChatConversation{}).Error
}

// UpsertBatch inserts messages; a row that already exists (student_id,
// client_id) keeps its content EXCEPT fields that are backfilled when the
// stored value is empty: thumb, image_url, homework_id and text. This lets the
// app push an attachment message IMMEDIATELY (so it can never be lost) and
// patch in the server URL / transcript / homework link afterwards. Idempotent.
func (r *ChatMessageRepository) UpsertBatch(msgs []model.ChatMessage) error {
	if len(msgs) == 0 {
		return nil
	}
	fill := func(col string) any {
		return gorm.Expr(
			"CASE WHEN chat_messages." + col + " = '' OR chat_messages." + col +
				" IS NULL THEN excluded." + col + " ELSE chat_messages." + col + " END")
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "student_id"}, {Name: "client_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"thumb":     fill("thumb"),
			"image_url": fill("image_url"),
			"text":      fill("text"),
			"homework_id": gorm.Expr(
				"CASE WHEN chat_messages.homework_id = 0 OR chat_messages.homework_id IS NULL " +
					"THEN excluded.homework_id ELSE chat_messages.homework_id END"),
		}),
	}).CreateInBatches(msgs, 100).Error
}
