package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

// Errors surfaced to the handlers as 400s.
var (
	ErrEmailTaken      = errors.New("an admin with this email already exists")
	ErrWrongPassword   = errors.New("current password is incorrect")
	ErrBadResetCode    = errors.New("invalid or expired reset code")
	ErrTooManyAttempts = errors.New("too many attempts — request a new code")
	ErrLastSuperAdmin  = errors.New("cannot remove the last active super admin")
	ErrEmailNotSent    = errors.New("email is not configured — set up SMTP in Settings first")
	ErrResendThrottled = errors.New("please wait a minute before requesting another code")
)

// AdminUserService manages admin accounts: create with an emailed temporary
// password, role assignment, own-password change, and the forgot/reset flow.
type AdminUserService struct {
	admins *repository.AdminRepository
	roles  *repository.AdminRoleRepository
	emails *email.Publisher
	rdb    *redis.Client
	// panelURL is the admin-panel login address included in emails ('' = omit).
	panelURL string
}

// NewAdminUserService builds an AdminUserService.
func NewAdminUserService(
	admins *repository.AdminRepository,
	roles *repository.AdminRoleRepository,
	emails *email.Publisher,
	rdb *redis.Client,
	panelURL string,
) *AdminUserService {
	if panelURL != "" {
		panelURL = strings.TrimRight(panelURL, "/") + "/#/admin/login"
	}
	return &AdminUserService{
		admins: admins, roles: roles, emails: emails, rdb: rdb, panelURL: panelURL,
	}
}

// CreateResult is returned when an admin account is created / password-reset.
type CreateResult struct {
	Admin *model.Admin
	// TempPassword is set ONLY when the credentials email could not be sent
	// (SMTP off) so the creating admin can pass it on manually.
	TempPassword string
	Emailed      bool
}

// Create makes a new admin account with a generated temporary password and
// emails the credentials to the new admin.
func (s *AdminUserService) Create(name, emailAddr string, roleID *uint) (*CreateResult, error) {
	emailAddr = strings.ToLower(strings.TrimSpace(emailAddr))
	name = strings.TrimSpace(name)
	if _, err := s.admins.FindByEmail(emailAddr); err == nil {
		return nil, ErrEmailTaken
	}
	if roleID != nil {
		if _, err := s.roles.FindByID(*roleID); err != nil {
			return nil, errors.New("role not found")
		}
	}

	password, err := generatePassword(12)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	admin := &model.Admin{
		Name:         name,
		Email:        emailAddr,
		PasswordHash: string(hash),
		Role:         "admin",
		RoleID:       roleID,
		IsActive:     true,
	}
	if err := s.admins.Create(admin); err != nil {
		return nil, err
	}
	created, err := s.admins.FindByID(admin.ID) // reload with role
	if err == nil {
		admin = created
	}
	admin.ApplyRole()

	emailed := s.sendCredentials(admin.Name, admin.Email, password, false)
	res := &CreateResult{Admin: admin, Emailed: emailed}
	if !emailed {
		res.TempPassword = password
	}
	return res, nil
}

// ResetPassword generates a fresh temporary password for an admin (a super/
// manager action from the users list — NOT the self-service forgot flow) and
// emails it to them.
func (s *AdminUserService) ResetPassword(id uint) (*CreateResult, error) {
	admin, err := s.admins.FindByID(id)
	if err != nil {
		return nil, err
	}
	password, err := generatePassword(12)
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	if err := s.admins.UpdatePassword(id, string(hash)); err != nil {
		return nil, err
	}
	emailed := s.sendCredentials(admin.Name, admin.Email, password, true)
	res := &CreateResult{Admin: admin, Emailed: emailed}
	if !emailed {
		res.TempPassword = password
	}
	return res, nil
}

// ChangePassword updates the signed-in admin's own password after verifying
// the current one.
func (s *AdminUserService) ChangePassword(adminID uint, current, next string) error {
	admin, err := s.admins.FindByID(adminID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(current)) != nil {
		return ErrWrongPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.admins.UpdatePassword(adminID, string(hash))
}

// ─── Forgot / reset password (login page) ───────────────────────────────────

const (
	resetCodeTTL      = 15 * time.Minute
	resetResendWindow = 60 * time.Second
	resetMaxAttempts  = 5
)

func resetKey(email string) string    { return "admin:pwreset:" + email }
func resetTryKey(email string) string { return "admin:pwreset:tries:" + email }
func resetThrKey(email string) string { return "admin:pwreset:throttle:" + email }

// Forgot emails a 6-digit reset code to the admin. It always reports success
// to the caller for unknown emails (no account enumeration); it errors only
// when email isn't configured or the resend throttle is hit.
func (s *AdminUserService) Forgot(ctx context.Context, emailAddr string) error {
	emailAddr = strings.ToLower(strings.TrimSpace(emailAddr))
	admin, err := s.admins.FindByEmail(emailAddr)
	if err != nil {
		return nil // pretend success — don't reveal which emails exist
	}
	if !admin.IsActive {
		return nil
	}
	if !s.emails.Enabled() {
		return ErrEmailNotSent
	}
	// 45s-style resend throttle (SETNX — works on old Redis).
	ok, err := s.rdb.SetNX(ctx, resetThrKey(emailAddr), "1", resetResendWindow).Result()
	if err == nil && !ok {
		return ErrResendThrottled
	}

	code, err := generateDigits(6)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, resetKey(emailAddr), code, resetCodeTTL).Err(); err != nil {
		return err
	}
	s.rdb.Del(ctx, resetTryKey(emailAddr))

	body := fmt.Sprintf(
		`<p>Hi %s,</p>
<p>Use this code to reset your Vaha AI admin password:</p>
<p style="font-size:28px;font-weight:800;letter-spacing:6px;margin:18px 0;">%s</p>
<p>The code expires in 15 minutes. If you didn't request this, you can ignore this email — your password is unchanged.</p>`,
		htmlEscape(admin.Name), code)
	return s.emails.Enqueue(email.Job{
		To:      emailAddr,
		Subject: "Your Vaha AI admin password reset code",
		HTML:    email.Wrap("Password reset code", body),
	})
}

// Reset verifies the emailed code and sets the new password.
func (s *AdminUserService) Reset(ctx context.Context, emailAddr, code, newPassword string) error {
	emailAddr = strings.ToLower(strings.TrimSpace(emailAddr))
	code = strings.TrimSpace(code)

	// Attempt limit so a code can't be brute-forced within its TTL.
	tries, err := s.rdb.Incr(ctx, resetTryKey(emailAddr)).Result()
	if err == nil && tries == 1 {
		s.rdb.Expire(ctx, resetTryKey(emailAddr), resetCodeTTL)
	}
	if tries > resetMaxAttempts {
		s.rdb.Del(ctx, resetKey(emailAddr))
		return ErrTooManyAttempts
	}

	stored, err := s.rdb.Get(ctx, resetKey(emailAddr)).Result()
	if err != nil || stored == "" {
		return ErrBadResetCode
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(code)) != 1 {
		return ErrBadResetCode
	}
	admin, err := s.admins.FindByEmail(emailAddr)
	if err != nil {
		return ErrBadResetCode
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.admins.UpdatePassword(admin.ID, string(hash)); err != nil {
		return err
	}
	// Single-use: burn the code + counters.
	s.rdb.Del(ctx, resetKey(emailAddr), resetTryKey(emailAddr), resetThrKey(emailAddr))
	return nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

// sendCredentials emails the username + temporary password. Returns false when
// email isn't configured (caller then surfaces the password once, in-response).
func (s *AdminUserService) sendCredentials(name, to, password string, isReset bool) bool {
	if !s.emails.Enabled() {
		return false
	}
	heading := "Your admin account is ready"
	intro := "An admin account has been created for you on the Vaha AI admin panel."
	if isReset {
		heading = "Your admin password was reset"
		intro = "Your Vaha AI admin password has been reset by an administrator."
	}
	link := ""
	if s.panelURL != "" {
		link = fmt.Sprintf(`<p><a href="%s" style="color:#123F36;font-weight:700;">Open the admin panel</a></p>`, s.panelURL)
	}
	body := fmt.Sprintf(
		`<p>Hi %s,</p>
<p>%s Sign in with:</p>
<p style="margin:16px 0;">
  <b>Username:</b> %s<br>
  <b>Temporary password:</b> <span style="font-family:monospace;font-size:16px;">%s</span>
</p>
%s
<p>Please change this password after your first sign-in (Settings → Account).</p>`,
		htmlEscape(name), intro, htmlEscape(to), htmlEscape(password), link)
	err := s.emails.Enqueue(email.Job{
		To:      to,
		Subject: "Vaha AI admin panel — your sign-in details",
		HTML:    email.Wrap(heading, body),
	})
	return err == nil
}

const passwordAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%"

// generatePassword returns a random password (unambiguous characters).
func generatePassword(n int) (string, error) {
	out := make([]byte, n)
	max := big.NewInt(int64(len(passwordAlphabet)))
	for i := range out {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = passwordAlphabet[idx.Int64()]
	}
	return string(out), nil
}

// generateDigits returns a random n-digit numeric code.
func generateDigits(n int) (string, error) {
	out := make([]byte, n)
	ten := big.NewInt(10)
	for i := range out {
		d, err := rand.Int(rand.Reader, ten)
		if err != nil {
			return "", err
		}
		out[i] = byte('0' + d.Int64())
	}
	return string(out), nil
}

// htmlEscape covers the few characters that matter inside our trusted template.
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
