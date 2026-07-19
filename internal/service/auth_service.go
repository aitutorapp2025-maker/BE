// Package service holds business logic, sitting between handlers and repositories.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/auth"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/cryptox"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/session"
	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidCredentials is returned for a wrong email/password or inactive account.
var ErrInvalidCredentials = errors.New("invalid email or password")

// LoginResult is returned on a successful login / refresh.
type LoginResult struct {
	Token         string // short-lived access JWT
	RefreshToken  string // single-use rotating refresh token
	SigningSecret string // per-session HMAC secret (login only; empty on refresh)
	ServerPub     string // X25519 server public key for E2E key exchange (login only)
	ExpiresAt     time.Time
	Admin         model.Admin
}

// AuthService handles authentication use cases.
type AuthService struct {
	admins   *repository.AdminRepository
	sessions *session.Store
	cfg      config.Config
}

// NewAuthService builds an AuthService.
func NewAuthService(admins *repository.AdminRepository, sessions *session.Store, cfg config.Config) *AuthService {
	return &AuthService{admins: admins, sessions: sessions, cfg: cfg}
}

// AdminLogin verifies credentials, opens a session and issues signed-request
// creds. If clientPub (base64 X25519) is provided, it also performs the E2E key
// exchange and returns the server public key.
func (s *AuthService) AdminLogin(ctx context.Context, email, password, clientPub string) (*LoginResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	admin, err := s.admins.FindByEmail(email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Run a dummy compare to keep timing similar and avoid user enumeration.
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidin"), []byte(password))
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if !admin.IsActive {
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	sess, err := s.sessions.Create(ctx, admin.ID)
	if err != nil {
		return nil, err
	}
	token, expiresAt, err := auth.GenerateAdminToken(
		s.cfg.JWT.Secret, s.cfg.JWT.AccessTTL, *admin, sess.ID)
	if err != nil {
		return nil, err
	}

	// E2E key exchange (optional — enables encrypted payloads for this session).
	var serverPub string
	if strings.TrimSpace(clientPub) != "" {
		aesKey, sPub, herr := cryptox.ServerHandshake(clientPub)
		if herr == nil {
			if e := s.sessions.SetEncKey(ctx, sess.ID, aesKey); e == nil {
				serverPub = sPub
			}
		}
	}

	_ = s.admins.TouchLastLogin(admin.ID)

	return &LoginResult{
		Token:         token,
		RefreshToken:  sess.RefreshToken,
		SigningSecret: sess.SigningSecret,
		ServerPub:     serverPub,
		ExpiresAt:     expiresAt,
		Admin:         *admin,
	}, nil
}

// Refresh rotates a refresh token and issues a new access token for the session.
// The signing secret is unchanged (not returned).
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*LoginResult, error) {
	sid, newRefresh, err := s.sessions.Rotate(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	adminID, err := s.sessions.AdminID(ctx, sid)
	if err != nil {
		return nil, session.ErrInvalidRefresh
	}
	admin, err := s.admins.FindByID(adminID)
	if err != nil || !admin.IsActive {
		return nil, session.ErrInvalidRefresh
	}
	token, expiresAt, err := auth.GenerateAdminToken(
		s.cfg.JWT.Secret, s.cfg.JWT.AccessTTL, *admin, sid)
	if err != nil {
		return nil, err
	}
	return &LoginResult{
		Token:        token,
		RefreshToken: newRefresh,
		ExpiresAt:    expiresAt,
		Admin:        *admin,
	}, nil
}

// Logout ends the given session.
func (s *AuthService) Logout(ctx context.Context, sid string) error {
	return s.sessions.Revoke(ctx, sid)
}
