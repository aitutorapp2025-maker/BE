package model

import "time"

// Setting is the single-row application settings record (id is always 1).
type Setting struct {
	ID                 uint      `gorm:"primaryKey" json:"id"`
	AppName            string    `gorm:"size:120;not null;default:'Vaha AI'" json:"app_name"`
	SupportEmail       string    `gorm:"size:190" json:"support_email"`
	EmailNotifications bool      `gorm:"not null;default:true" json:"email_notifications"`
	AutoApproveAnswers bool      `gorm:"not null;default:false" json:"auto_approve_answers"`
	MaintenanceMode    bool      `gorm:"not null;default:false" json:"maintenance_mode"`

	// Outgoing email (SMTP). Password is never serialized to the client.
	SmtpEnabled  bool   `gorm:"not null;default:false" json:"smtp_enabled"`
	SmtpHost     string `gorm:"size:190" json:"smtp_host"`
	SmtpPort     string `gorm:"size:10" json:"smtp_port"`
	SmtpUser     string `gorm:"size:190" json:"smtp_user"`
	SmtpPassword string `gorm:"size:255" json:"-"`
	SmtpFrom     string `gorm:"size:190" json:"smtp_from"`
	SmtpFromName string `gorm:"size:120" json:"smtp_from_name"`

	// Computed (not stored): whether a password is on file.
	SmtpPasswordSet bool `gorm:"-" json:"smtp_password_set"`

	// Error alerting — email sent when a server error (5xx / panic) occurs.
	ErrorAlertsEnabled bool   `gorm:"not null;default:false" json:"error_alerts_enabled"`
	AlertEmail         string `gorm:"size:190" json:"alert_email"`

	// SMS (outgoing text messages). Secrets are never serialized to the client.
	SmsEnabled     bool   `gorm:"not null;default:false" json:"sms_enabled"`
	SmsProvider    string `gorm:"size:20;not null;default:nexmo" json:"sms_provider"` // nexmo | smsexpert
	SmsCountryCode string `gorm:"size:5;not null;default:91" json:"sms_country_code"`

	// Nexmo / Vonage
	NexmoAPIKey    string `gorm:"size:120" json:"nexmo_api_key"`
	NexmoAPISecret string `gorm:"size:190" json:"-"`
	NexmoFrom      string `gorm:"size:60" json:"nexmo_from"`

	// SMS Expert (itagg HTTP gateway). Request params: usr, pwd, from, to,
	// type, route, txt.
	SmsExpertAPIURL   string `gorm:"size:400" json:"smsexpert_api_url"` // base url
	SmsExpertUser     string `gorm:"size:120" json:"smsexpert_user"`    // usr
	SmsExpertPassword string `gorm:"size:190" json:"-"`                 // pwd
	SmsExpertSender   string `gorm:"size:60" json:"smsexpert_sender"`   // from
	SmsExpertRoute    string `gorm:"size:20" json:"smsexpert_route"`    // route
	SmsExpertType     string `gorm:"size:20" json:"smsexpert_type"`     // type

	// Computed (not stored): whether each secret is on file.
	NexmoSecretSet       bool `gorm:"-" json:"nexmo_secret_set"`
	SmsExpertPasswordSet bool `gorm:"-" json:"smsexpert_password_set"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (Setting) TableName() string { return "settings" }
