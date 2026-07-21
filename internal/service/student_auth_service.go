package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/auth"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/cryptox"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/session"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/sms"
)

// Student OTP-login errors.
var (
	ErrInvalidPhone = errors.New("please enter a valid 10-digit mobile number")
	ErrOTPThrottled = errors.New("an OTP was just sent — please wait a moment before trying again")
	ErrInvalidOTP   = errors.New("incorrect or expired code")
)

const (
	otpTTL          = 5 * time.Minute
	otpResendWindow = 45 * time.Second
)

// StudentAuthService handles passwordless (OTP-over-SMS) student login for the
// mobile app: it generates + delivers codes and issues student JWTs.
type StudentAuthService struct {
	students *repository.StudentRepository
	devices  *repository.DeviceTokenRepository
	sessions *session.Store
	sms      *sms.Publisher
	cfg      config.Config
}

// NewStudentAuthService builds a StudentAuthService.
func NewStudentAuthService(students *repository.StudentRepository,
	devices *repository.DeviceTokenRepository, sessions *session.Store,
	smsPub *sms.Publisher, cfg config.Config) *StudentAuthService {
	return &StudentAuthService{
		students: students,
		devices:  devices,
		sessions: sessions,
		sms:      smsPub,
		cfg:      cfg,
	}
}

// RegisterDevice records an FCM push token at app open (before login). The token
// is stored unmapped; it is later associated with a student on OTP verify.
func (s *StudentAuthService) RegisterDevice(ctx context.Context, token, platform string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return s.devices.Register(token, strings.TrimSpace(platform))
}

// StudentAuthResult is returned on a successful OTP verification. It carries the
// signed-session credentials (signing secret + E2E server public key) so the
// mobile app can sign + encrypt subsequent requests, exactly like the admin.
type StudentAuthResult struct {
	Token         string
	SigningSecret string // per-session HMAC secret for request signing
	ServerPub     string // X25519 server public key for E2E key exchange
	ExpiresAt     time.Time
	Student       model.Student
	IsNew         bool
}

// SendOTP issues a verification code for a phone number (stored 5 min).
//
//   - Development: uses the fixed OTPStatic code and does NOT contact the SMS
//     gateway. The code is returned so login can be tested offline.
//   - Production: generates a real random code and delivers it by SMS via the
//     admin-configured provider; the code is never returned.
func (s *StudentAuthService) SendOTP(ctx context.Context, phone string) (devCode string, err error) {
	p := canonPhone(phone)
	if p == "" {
		return "", ErrInvalidPhone
	}
	// Rate-limit resends per phone.
	allowed, err := s.sessions.OTPThrottle(ctx, p, otpResendWindow)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", ErrOTPThrottled
	}

	// Development: fixed code, no SMS sent.
	if !s.cfg.IsProduction() {
		code := s.cfg.OTPStatic
		if code == "" {
			code = "202627"
		}
		if err := s.sessions.SetOTP(ctx, p, code, otpTTL); err != nil {
			return "", err
		}
		return code, nil
	}

	// Production: real random code delivered by SMS.
	code := gen6()
	if err := s.sessions.SetOTP(ctx, p, code, otpTTL); err != nil {
		return "", err
	}
	// Short message (<=140 bytes) with the code first, so Android's SMS User
	// Consent API can auto-detect it. Delivered through RabbitMQ → SMS worker.
	text := fmt.Sprintf("%s is your Vaha AI verification code. Valid for 5 minutes.", code)
	if err := s.sms.Enqueue(sms.Job{To: p, Text: text}); err != nil {
		return "", err
	}
	return "", nil
}

// VerifyOTP checks the code and, on success, finds or creates the student by
// phone, stores the device token (if given), opens a signed session and issues
// a student JWT. When clientPub (base64 X25519) is present it also performs the
// E2E key exchange and returns the server public key.
func (s *StudentAuthService) VerifyOTP(ctx context.Context, phone, code, deviceToken, clientPub string) (*StudentAuthResult, error) {
	p := canonPhone(phone)
	if p == "" {
		return nil, ErrInvalidPhone
	}
	ok, err := s.sessions.ConsumeOTP(ctx, p, strings.TrimSpace(code))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrInvalidOTP
	}

	student, err := s.students.FindByPhone(p)
	isNew := false
	switch {
	case errors.Is(err, repository.ErrNotFound):
		// First-time login: create a minimal student; profile is filled in later.
		student = &model.Student{
			Phone:     p,
			Plan:      "trial",
			PayStatus: "trial",
			JoinedAt:  time.Now(),
		}
		if err := s.students.Create(student); err != nil {
			return nil, err
		}
		isNew = true
	case err != nil:
		return nil, err
	}

	// Map this device's push token to the student + mobile number. The token was
	// (usually) already registered at app open; here it gets its student/phone.
	if deviceToken = strings.TrimSpace(deviceToken); deviceToken != "" {
		_ = s.devices.Map(deviceToken, "", student.ID, student.Phone)
	}

	// Open a signed session (per-session HMAC secret) so subsequent requests are
	// signed + replay-protected, just like the admin.
	sess, err := s.sessions.CreateStudent(ctx, student.ID, s.cfg.JWT.StudentTTL)
	if err != nil {
		return nil, err
	}
	token, exp, err := auth.GenerateStudentToken(s.cfg.JWT.Secret, s.cfg.JWT.StudentTTL, *student, sess.ID)
	if err != nil {
		return nil, err
	}

	// E2E key exchange (optional — enables encrypted payloads for this session).
	var serverPub string
	if strings.TrimSpace(clientPub) != "" {
		aesKey, sPub, herr := cryptox.ServerHandshake(clientPub)
		if herr == nil {
			if e := s.sessions.SetEncKeyTTL(ctx, sess.ID, aesKey, s.cfg.JWT.StudentTTL); e == nil {
				serverPub = sPub
			}
		}
	}

	return &StudentAuthResult{
		Token:         token,
		SigningSecret: sess.SigningSecret,
		ServerPub:     serverPub,
		ExpiresAt:     exp,
		Student:       *student,
		IsNew:         isNew,
	}, nil
}

// StudentProfileInput carries the editable profile fields for a student.
type StudentProfileInput struct {
	Name             string
	StudentClass     string
	Board            string
	Medium           string
	TeachingLanguage string
	ParentPhone      string
}

// GetStudent returns a student by id (for the signed-in student to load their
// own account).
func (s *StudentAuthService) GetStudent(ctx context.Context, studentID uint) (*model.Student, error) {
	return s.students.FindByID(studentID)
}

// UpdateProfile saves the signed-in student's profile (name, class, board,
// medium, parent phone) so it persists on the server — surviving a reinstall.
func (s *StudentAuthService) UpdateProfile(ctx context.Context, studentID uint, in StudentProfileInput) (*model.Student, error) {
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, err
	}
	st.Name = strings.TrimSpace(in.Name)
	st.StudentClass = strings.TrimSpace(in.StudentClass)
	st.Board = strings.TrimSpace(in.Board)
	st.Medium = strings.TrimSpace(in.Medium)
	st.TeachingLanguage = strings.TrimSpace(in.TeachingLanguage)
	st.ParentPhone = strings.TrimSpace(in.ParentPhone)
	if err := s.students.Update(st); err != nil {
		return nil, err
	}
	return st, nil
}

// SaveDeviceToken maps an FCM push token to a signed-in student + phone (e.g.
// when the token is fetched or refreshed after login).
func (s *StudentAuthService) SaveDeviceToken(ctx context.Context, studentID uint, phone, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return s.devices.Map(token, "", studentID, phone)
}

// gen6 returns a cryptographically-random 6-digit code (zero-padded).
func gen6() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "000000"
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// canonPhone reduces a raw phone number to a 10-digit Indian core (dropping a
// leading +91 / 91 / 0), used as the OTP key and the student's stored phone.
// Returns "" when it isn't a 10-digit number.
func canonPhone(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteByte(byte(r))
		}
	}
	d := b.String()
	if len(d) == 12 && strings.HasPrefix(d, "91") {
		d = d[2:]
	}
	if len(d) == 11 && strings.HasPrefix(d, "0") {
		d = d[1:]
	}
	if len(d) != 10 {
		return ""
	}
	return d
}
