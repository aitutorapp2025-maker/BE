package model

import "time"

// HomeBanner is an admin-managed promotional banner shown on the student app's
// Home screen. A banner can be an image, a text card (title/message), or an
// image with text overlaid — whichever fields are filled.
type HomeBanner struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Title    string `gorm:"size:120" json:"title"`
	Message  string `gorm:"size:300" json:"message"`
	ImageURL string `gorm:"size:500" json:"image_url"`
	// Active banners appear in the app, ordered by SortOrder (then newest).
	Active    bool      `gorm:"not null" json:"active"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (HomeBanner) TableName() string { return "home_banners" }
