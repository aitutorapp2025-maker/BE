package model

import "time"

// AnalyticsDaily is one calendar day of Firebase (GA4) analytics, aggregated
// from the BigQuery export by the daily sync so the admin dashboard reads from
// our own DB. The breakdown columns hold small JSON blobs.
type AnalyticsDaily struct {
	Day         string `gorm:"primaryKey;size:10" json:"day"` // YYYY-MM-DD
	ActiveUsers int    `gorm:"not null;default:0" json:"active_users"`
	NewUsers    int    `gorm:"not null;default:0" json:"new_users"`
	Sessions    int    `gorm:"not null;default:0" json:"sessions"`
	EventCount  int    `gorm:"not null;default:0" json:"event_count"`

	// JSON blobs: TopEvents [{name,count}], HourlyEvents [24]int (by hour of
	// day, 0-23), Platforms [{name,count}].
	TopEvents    string `gorm:"type:text" json:"-"`
	HourlyEvents string `gorm:"type:text" json:"-"`
	Platforms    string `gorm:"type:text" json:"-"`

	UpdatedAt time.Time `json:"updated_at"`
}

// CrashDaily is one calendar day of Firebase Crashlytics, aggregated from the
// BigQuery export by the daily sync.
type CrashDaily struct {
	Day            string  `gorm:"primaryKey;size:10" json:"day"` // YYYY-MM-DD
	Crashes        int     `gorm:"not null;default:0" json:"crashes"`
	AffectedUsers  int     `gorm:"not null;default:0" json:"affected_users"`
	CrashFreeUsers float64 `gorm:"not null;default:0" json:"crash_free_users"` // percentage 0-100

	// JSON blobs: TopIssues [{title,count,issue_id}], HourlyCrashes [24]int,
	// Versions [{name,count}].
	TopIssues     string `gorm:"type:text" json:"-"`
	HourlyCrashes string `gorm:"type:text" json:"-"`
	Versions      string `gorm:"type:text" json:"-"`

	UpdatedAt time.Time `json:"updated_at"`
}
