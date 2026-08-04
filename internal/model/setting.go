package model

import "time"

// Setting is the single-row application settings record (id is always 1).
type Setting struct {
	ID                 uint   `gorm:"primaryKey" json:"id"`
	AppName            string `gorm:"size:120;not null;default:'Vaha AI'" json:"app_name"`
	SupportEmail       string `gorm:"size:190" json:"support_email"`
	LogoURL            string `gorm:"size:400" json:"logo_url"` // organisation logo (uploaded asset URL)
	EmailNotifications bool   `gorm:"not null;default:true" json:"email_notifications"`
	AutoApproveAnswers bool   `gorm:"not null;default:false" json:"auto_approve_answers"`
	MaintenanceMode    bool   `gorm:"not null;default:false" json:"maintenance_mode"` // legacy (unused)

	// Maintenance mode — separate toggles for the web and mobile customer apps.
	// When on, customers see MaintenanceTitle/Message; the admin panel is never
	// blocked. The message is edited from the admin "Maintenance" settings form.
	MaintenanceWeb     bool   `gorm:"not null;default:false" json:"maintenance_web"`
	MaintenanceMobile  bool   `gorm:"not null;default:false" json:"maintenance_mobile"`
	MaintenanceTitle   string `gorm:"size:120" json:"maintenance_title"`
	MaintenanceMessage string `gorm:"size:600" json:"maintenance_message"`

	// Mobile app version / force update. The app compares its own version to
	// LatestAppVersion; below MinAppVersion (or when ForceUpdate is on) the
	// update is mandatory (no skip), otherwise the app shows a skippable prompt.
	LatestAppVersion string `gorm:"size:20" json:"latest_app_version"`
	MinAppVersion    string `gorm:"size:20" json:"min_app_version"`
	ForceUpdate      bool   `gorm:"not null;default:false" json:"force_update"`
	UpdateMessage    string `gorm:"size:400" json:"update_message"`
	AndroidStoreURL  string `gorm:"size:300" json:"android_store_url"`
	IosStoreURL      string `gorm:"size:300" json:"ios_store_url"`

	// FcmServiceAccount is the uploaded Firebase service-account JSON used to
	// send push notifications. Write-only (never serialized); FcmConfigured tells
	// the admin whether one is on file.
	FcmServiceAccount string `gorm:"type:text" json:"-"`
	FcmConfigured     bool   `gorm:"-" json:"fcm_configured"`

	// Google (Gmail) SSO. When enabled AND a client id is set, the login screen
	// shows "Continue with Google". The client id is the public OAuth Web
	// client id (not a secret).
	GoogleSsoEnabled bool   `gorm:"not null;default:false" json:"google_sso_enabled"`
	GoogleClientID   string `gorm:"size:200" json:"google_client_id"`

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

	// CAPTCHA (bot protection on the public contact form). Secret is never
	// serialized to the client; site key is public.
	CaptchaEnabled  bool   `gorm:"not null;default:false" json:"captcha_enabled"`
	CaptchaProvider string `gorm:"size:20;not null;default:recaptcha" json:"captcha_provider"` // recaptcha | hcaptcha | turnstile
	CaptchaSiteKey  string `gorm:"size:190" json:"captcha_site_key"`
	CaptchaSecret   string `gorm:"size:190" json:"-"`

	// AI tutor (RAG). Keys are never serialized to the client; model names are.
	AIEnabled       bool   `gorm:"not null;default:false" json:"ai_enabled"`
	AnthropicAPIKey string `gorm:"size:255" json:"-"`
	AnthropicModel  string `gorm:"size:60" json:"anthropic_model"` // e.g. claude-sonnet-5
	VoyageAPIKey    string `gorm:"size:255" json:"-"`
	VoyageModel     string `gorm:"size:60" json:"voyage_model"` // e.g. voyage-3

	// Razorpay (UPI AutoPay subscriptions). key_id is used client-side so it's
	// returned; the key secret and webhook secret are server-only (never
	// serialized).
	RazorpayEnabled       bool   `gorm:"not null;default:false" json:"razorpay_enabled"`
	RazorpayKeyID         string `gorm:"size:80" json:"razorpay_key_id"`
	RazorpayKeySecret     string `gorm:"size:120" json:"-"`
	RazorpayWebhookSecret string `gorm:"size:120" json:"-"`

	// AutopayEnabled controls whether trials require a UPI-AutoPay mandate. When
	// off, new customers are never prompted to enable autopay (the trial is
	// usable without a mandate). Defaults to on to preserve existing behaviour.
	AutopayEnabled bool `gorm:"not null;default:true" json:"autopay_enabled"`

	// CastEnabled shows the "Cast to TV" (screen mirror) option on the app home
	// page. Off by default.
	CastEnabled bool `gorm:"not null;default:false" json:"cast_enabled"`

	// TimedTasksEnabled turns on per-task countdown timers: each homework task
	// runs a timer for its suggested duration and alerts the student when it ends
	// ("time up — start the next task"). Off by default.
	TimedTasksEnabled bool `gorm:"not null;default:false" json:"timed_tasks_enabled"`

	// Computed (not stored): whether each secret is on file.
	NexmoSecretSet       bool `gorm:"-" json:"nexmo_secret_set"`
	SmsExpertPasswordSet bool `gorm:"-" json:"smsexpert_password_set"`
	CaptchaSecretSet     bool `gorm:"-" json:"captcha_secret_set"`
	AnthropicKeySet      bool `gorm:"-" json:"anthropic_key_set"`
	VoyageKeySet         bool `gorm:"-" json:"voyage_key_set"`
	RazorpaySecretSet    bool `gorm:"-" json:"razorpay_secret_set"`
	RazorpayWebhookSet   bool `gorm:"-" json:"razorpay_webhook_set"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName sets the table name explicitly.
func (Setting) TableName() string { return "settings" }
