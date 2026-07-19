package model

import "time"

// Plan is a subscription plan managed from the admin panel.
type Plan struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:80;not null" json:"name"`
	PriceRupees  int       `gorm:"not null;default:0" json:"price_rupees"`
	MrpRupees    *int      `json:"mrp_rupees"` // original price when discounted (nullable)
	DurationDays int       `gorm:"not null;default:30" json:"duration_days"`
	Tagline      string    `gorm:"size:120" json:"tagline"`
	Features     []string  `gorm:"serializer:json" json:"features"`
	BestValue    bool      `gorm:"not null;default:false" json:"best_value"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (Plan) TableName() string { return "plans" }
