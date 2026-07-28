package model

import (
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
)

// BookChunk is one embedded passage of a textbook — the unit retrieved to ground
// a student's answer. Chunks carry a denormalized copy of the book's class /
// subject / medium so retrieval can filter by the student's profile without a
// join. The embedding lives in a pgvector column (cosine distance `<=>`).
type BookChunk struct {
	ID     uint `gorm:"primaryKey" json:"id"`
	BookID uint `gorm:"not null;index" json:"book_id"`

	ClassName string `gorm:"size:40;not null;index:idx_book_chunks_scope" json:"class_name"`
	Subject   string `gorm:"size:80;not null;index:idx_book_chunks_scope" json:"subject"`
	Medium    string `gorm:"size:20;not null;index:idx_book_chunks_scope" json:"medium"`
	BookTitle string `gorm:"size:200" json:"book_title"`

	ChunkIndex int       `gorm:"not null" json:"chunk_index"`
	Content    string    `gorm:"type:text;not null" json:"content"`
	Embedding  ai.Vector `gorm:"type:vector(1024)" json:"-"`

	CreatedAt time.Time `json:"created_at"`
}

// TableName sets the table name explicitly.
func (BookChunk) TableName() string { return "book_chunks" }
