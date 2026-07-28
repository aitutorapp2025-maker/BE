package repository

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// BookChunkRepository stores and retrieves embedded textbook passages.
type BookChunkRepository struct {
	db *gorm.DB
}

// NewBookChunkRepository builds a BookChunkRepository.
func NewBookChunkRepository(db *gorm.DB) *BookChunkRepository {
	return &BookChunkRepository{db: db}
}

// DeleteByBook removes all chunks for a book (used before a re-ingest so a book
// is never indexed twice).
func (r *BookChunkRepository) DeleteByBook(bookID uint) error {
	return r.db.Where("book_id = ?", bookID).Delete(&model.BookChunk{}).Error
}

// Insert saves a batch of chunks.
func (r *BookChunkRepository) Insert(chunks []model.BookChunk) error {
	if len(chunks) == 0 {
		return nil
	}
	return r.db.CreateInBatches(chunks, 50).Error
}

// CountByBook returns how many chunks a book currently has.
func (r *BookChunkRepository) CountByBook(bookID uint) (int64, error) {
	var n int64
	err := r.db.Model(&model.BookChunk{}).Where("book_id = ?", bookID).Count(&n).Error
	return n, err
}

// RetrievedChunk is a passage returned from a similarity search, with its
// cosine distance (smaller = closer).
type RetrievedChunk struct {
	BookTitle string  `json:"book_title"`
	Subject   string  `json:"subject"`
	Content   string  `json:"content"`
	Distance  float64 `json:"distance"`
}

// Search returns the closest chunks to the query embedding, scoped to a class +
// medium (and optionally a subject). Cosine distance uses the pgvector `<=>`
// operator; the ivfflat/HNSW index is optional at this scale, so a sequential
// scan is acceptable for the first books.
func (r *BookChunkRepository) Search(query ai.Vector, className, medium, subject string, topK int) ([]RetrievedChunk, error) {
	if topK <= 0 {
		topK = 6
	}
	q := r.db.Model(&model.BookChunk{}).
		Select("book_title, subject, content, (embedding <=> ?) AS distance", query.Literal()).
		Where("class_name = ? AND medium = ?", className, medium)
	if subject != "" {
		q = q.Where("subject = ?", subject)
	}
	var out []RetrievedChunk
	err := q.Order("distance ASC").Limit(topK).Scan(&out).Error
	return out, err
}
