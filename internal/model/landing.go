package model

import "time"

// LandingNavItem is a nav-bar menu link on the public landing page. Toggling
// Enabled hides both the menu link and its section.
type LandingNavItem struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Label      string    `gorm:"size:60;not null" json:"label"`
	SectionKey string    `gorm:"size:40;not null" json:"section_key"` // features/how/pricing/reviews/faq/contact
	Enabled    bool      `gorm:"not null;default:true" json:"enabled"`
	SortOrder  int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (LandingNavItem) TableName() string { return "landing_nav_items" }
func (m *LandingNavItem) SetID(id uint)  { m.ID = id }

// LandingStat is a single stat in the stats band ("10,000+" / "Students learning").
type LandingStat struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Value     string    `gorm:"size:40;not null" json:"value"`
	Label     string    `gorm:"size:80;not null" json:"label"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (LandingStat) TableName() string { return "landing_stats" }
func (m *LandingStat) SetID(id uint)  { m.ID = id }

// LandingFeature is a feature card. Icon is a key mapped to an icon on the FE.
type LandingFeature struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Icon        string    `gorm:"size:40;not null;default:'star'" json:"icon"`
	Title       string    `gorm:"size:120;not null" json:"title"`
	Description string    `gorm:"size:400;not null" json:"description"`
	SortOrder   int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (LandingFeature) TableName() string { return "landing_features" }
func (m *LandingFeature) SetID(id uint)  { m.ID = id }

// LandingTestimonial is a review card.
type LandingTestimonial struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:80;not null" json:"name"`
	Sub       string    `gorm:"size:120" json:"sub"` // "Parent · Class 8 · Madurai"
	Rating    int       `gorm:"not null;default:5" json:"rating"`
	Quote     string    `gorm:"size:600;not null" json:"quote"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (LandingTestimonial) TableName() string { return "landing_testimonials" }
func (m *LandingTestimonial) SetID(id uint)  { m.ID = id }

// LandingFaq is a question/answer pair.
type LandingFaq struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Question  string    `gorm:"size:200;not null" json:"question"`
	Answer    string    `gorm:"size:800;not null" json:"answer"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (LandingFaq) TableName() string { return "landing_faqs" }
func (m *LandingFaq) SetID(id uint)  { m.ID = id }

// LandingText is the singleton (id=1) of all editable scalar text on the page.
type LandingText struct {
	ID uint `gorm:"primaryKey" json:"id"`

	HeroBadge        string `gorm:"size:160" json:"hero_badge"`
	HeroTitle        string `gorm:"size:200" json:"hero_title"`
	HeroSubtitle     string `gorm:"size:500" json:"hero_subtitle"`
	HeroPrimaryCta   string `gorm:"size:60" json:"hero_primary_cta"`
	HeroSecondaryCta string `gorm:"size:60" json:"hero_secondary_cta"`
	HeroNote         string `gorm:"size:120" json:"hero_note"`

	FeaturesTitle string `gorm:"size:160" json:"features_title"`

	ReviewsTitle      string `gorm:"size:160" json:"reviews_title"`
	ReviewsRatingNote string `gorm:"size:80" json:"reviews_rating_note"`

	FaqTitle string `gorm:"size:120" json:"faq_title"`

	ContactTitle    string `gorm:"size:120" json:"contact_title"`
	ContactSubtitle string `gorm:"size:300" json:"contact_subtitle"`
	ContactEmail    string `gorm:"size:120" json:"contact_email"`
	ContactWhatsapp string `gorm:"size:60" json:"contact_whatsapp"`
	ContactHours    string `gorm:"size:120" json:"contact_hours"`
	ContactOffice   string `gorm:"size:160" json:"contact_office"`

	CtaTitle    string `gorm:"size:160" json:"cta_title"`
	CtaSubtitle string `gorm:"size:300" json:"cta_subtitle"`
	CtaButton   string `gorm:"size:60" json:"cta_button"`

	FooterDeveloper    string `gorm:"size:80" json:"footer_developer"`
	FooterDeveloperURL string `gorm:"size:200" json:"footer_developer_url"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (LandingText) TableName() string { return "landing_text" }

// LandingSeo is the singleton (id=1) of admin-managed SEO / social meta for the
// public landing page. It powers the <title>, meta description/keywords,
// canonical link, robots directive, Open Graph and Twitter Card tags, and an
// optional JSON-LD structured-data block. Empty fields fall back on the FE.
type LandingSeo struct {
	ID uint `gorm:"primaryKey" json:"id"`

	MetaTitle       string `gorm:"size:200" json:"meta_title"`
	MetaDescription string `gorm:"size:400" json:"meta_description"`
	MetaKeywords    string `gorm:"size:400" json:"meta_keywords"`
	CanonicalURL    string `gorm:"size:300" json:"canonical_url"`
	Robots          string `gorm:"size:80" json:"robots"`
	ThemeColor      string `gorm:"size:20" json:"theme_color"`

	OgTitle       string `gorm:"size:200" json:"og_title"`
	OgDescription string `gorm:"size:400" json:"og_description"`
	OgImage       string `gorm:"size:400" json:"og_image"`
	OgType        string `gorm:"size:40" json:"og_type"`
	OgURL         string `gorm:"size:300" json:"og_url"`
	OgSiteName    string `gorm:"size:120" json:"og_site_name"`

	TwitterCard        string `gorm:"size:40" json:"twitter_card"`
	TwitterSite        string `gorm:"size:60" json:"twitter_site"`
	TwitterTitle       string `gorm:"size:200" json:"twitter_title"`
	TwitterDescription string `gorm:"size:400" json:"twitter_description"`
	TwitterImage       string `gorm:"size:400" json:"twitter_image"`

	StructuredData string `gorm:"type:text" json:"structured_data"` // raw JSON-LD

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (LandingSeo) TableName() string { return "landing_seo" }
