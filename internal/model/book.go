package model

import "time"

// Book is a textbook loaded for a class + medium (a RAG knowledge-base source).
type Book struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Title     string    `gorm:"size:200;not null" json:"title"`
	ClassName string    `gorm:"size:40;not null" json:"class_name"` // "Class 10"
	Subject   string    `gorm:"size:80;not null" json:"subject"`
	Medium    string    `gorm:"size:20;not null" json:"medium"` // Tamil / English
	Publisher string    `gorm:"size:120" json:"publisher"`
	Status    string    `gorm:"size:20;not null;default:Pending" json:"status"` // Indexed / Pending / Processing
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (Book) TableName() string { return "books" }
