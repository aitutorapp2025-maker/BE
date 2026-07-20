// Package auth provides JWT issuing and parsing for authenticated users.
package auth

import (
	"errors"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken is returned when a token is malformed, expired or unsigned by us.
var ErrInvalidToken = errors.New("invalid or expired token")

// Claims are the custom JWT claims embedded in an admin token.
type Claims struct {
	AdminID uint   `json:"admin_id"`
	Email   string `json:"email"`
	Role    string `json:"role"`
	SID     string `json:"sid"` // session id (for the signing secret + nonces)
	jwt.RegisteredClaims
}

// GenerateAdminToken signs a JWT for the given admin + session, valid for ttl.
func GenerateAdminToken(secret string, ttl time.Duration, a model.Admin, sid string) (string, time.Time, error) {
	expiresAt := time.Now().Add(ttl)
	claims := Claims{
		AdminID: a.ID,
		Email:   a.Email,
		Role:    a.Role,
		SID:     sid,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   a.Email,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

// ParseToken validates a signed token string and returns its claims.
func ParseToken(secret, tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// StudentClaims are the custom JWT claims embedded in a student (mobile) token.
type StudentClaims struct {
	StudentID uint   `json:"student_id"`
	Phone     string `json:"phone"`
	jwt.RegisteredClaims
}

// GenerateStudentToken signs a JWT for the given student, valid for ttl.
func GenerateStudentToken(secret string, ttl time.Duration, s model.Student) (string, time.Time, error) {
	expiresAt := time.Now().Add(ttl)
	claims := StudentClaims{
		StudentID: s.ID,
		Phone:     s.Phone,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   s.Phone,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

// ParseStudentToken validates a signed student token string and returns its claims.
func ParseStudentToken(secret, tokenStr string) (*StudentClaims, error) {
	claims := &StudentClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
