// Package service holds business logic, sitting between handlers and repositories.
package service

import (
	"errors"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/auth"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidCredentials is returned for a wrong email/password or inactive account.
var ErrInvalidCredentials = errors.New("invalid email or password")

// LoginResult is returned on a successful login.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	Admin     model.Admin
}

// AuthService handles authentication use cases.
type AuthService struct {
	admins *repository.AdminRepository
	cfg    config.Config
}

// NewAuthService builds an AuthService.
func NewAuthService(admins *repository.AdminRepository, cfg config.Config) *AuthService {
	return &AuthService{admins: admins, cfg: cfg}
}

// AdminLogin verifies credentials and issues a JWT on success.
func (s *AuthService) AdminLogin(email, password string) (*LoginResult, error) {
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

	token, expiresAt, err := auth.GenerateAdminToken(s.cfg.JWT.Secret, s.cfg.JWT.TTL, *admin)
	if err != nil {
		return nil, err
	}

	// Best-effort: don't fail login if the timestamp update errors.
	_ = s.admins.TouchLastLogin(admin.ID)

	return &LoginResult{Token: token, ExpiresAt: expiresAt, Admin: *admin}, nil
}
