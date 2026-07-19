// Package session manages authenticated sessions in Redis: a per-session HMAC
// signing secret, single-use rotating refresh tokens, and one-time request
// nonces (replay protection).
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrInvalidRefresh is returned when a refresh token is unknown, expired, or has
// already been rotated (reuse).
var ErrInvalidRefresh = errors.New("invalid or reused refresh token")

// ErrNoSession is returned when a session (signing secret) no longer exists.
var ErrNoSession = errors.New("session not found")

// Session holds the values handed to the client at login/refresh.
type Session struct {
	ID           string // session id (embedded in the access JWT as "sid")
	SigningSecret string
	RefreshToken string
}

// Store is a Redis-backed session store.
type Store struct {
	rdb        *redis.Client
	refreshTTL time.Duration
}

// New builds a session Store.
func New(rdb *redis.Client, refreshTTL time.Duration) *Store {
	return &Store{rdb: rdb, refreshTTL: refreshTTL}
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func secretKey(sid string) string  { return "sess:secret:" + sid }
func adminKey(sid string) string   { return "sess:admin:" + sid }
func enckeyKey(sid string) string  { return "sess:enckey:" + sid }
func refreshKey(tok string) string { return "refresh:" + hashToken(tok) }
func nonceKey(sid, n string) string { return "nonce:" + sid + ":" + n }

// getDelScript atomically reads and deletes a key (GETDEL isn't available before
// Redis 6.2, but EVAL works on 2.6+).
var getDelScript = redis.NewScript(
	`local v = redis.call('GET', KEYS[1]); if v then redis.call('DEL', KEYS[1]) end; return v`)

// Create starts a new session for the given admin and returns its secrets.
func (s *Store) Create(ctx context.Context, adminID uint) (*Session, error) {
	sid := randHex(16)
	secret := randHex(32)
	refresh := randHex(32)

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, secretKey(sid), secret, s.refreshTTL)
	pipe.Set(ctx, adminKey(sid), adminID, s.refreshTTL)
	pipe.Set(ctx, refreshKey(refresh), sid, s.refreshTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	return &Session{ID: sid, SigningSecret: secret, RefreshToken: refresh}, nil
}

// Secret returns the signing secret for a session, or ErrNoSession.
func (s *Store) Secret(ctx context.Context, sid string) (string, error) {
	v, err := s.rdb.Get(ctx, secretKey(sid)).Result()
	if err == redis.Nil {
		return "", ErrNoSession
	}
	return v, err
}

// Rotate consumes a refresh token and issues a NEW refresh token for the same
// session (single use). Returns the session id + new refresh token. A token that
// isn't found is treated as invalid/reused.
func (s *Store) Rotate(ctx context.Context, refreshToken string) (sid, newRefresh string, err error) {
	// Atomically read + delete the refresh token, making it single-use.
	sid, err = getDelScript.Run(ctx, s.rdb, []string{refreshKey(refreshToken)}).Text()
	if err == redis.Nil {
		return "", "", ErrInvalidRefresh
	}
	if err != nil {
		return "", "", err
	}
	// Ensure the session still exists.
	if _, e := s.rdb.Get(ctx, secretKey(sid)).Result(); e == redis.Nil {
		return "", "", ErrInvalidRefresh
	} else if e != nil {
		return "", "", e
	}
	newRefresh = randHex(32)
	if e := s.rdb.Set(ctx, refreshKey(newRefresh), sid, s.refreshTTL).Err(); e != nil {
		return "", "", e
	}
	// Extend the session/secret TTL on activity.
	s.rdb.Expire(ctx, secretKey(sid), s.refreshTTL)
	s.rdb.Expire(ctx, adminKey(sid), s.refreshTTL)
	return sid, newRefresh, nil
}

// AdminID returns the admin id for a session.
func (s *Store) AdminID(ctx context.Context, sid string) (uint, error) {
	v, err := s.rdb.Get(ctx, adminKey(sid)).Uint64()
	if err == redis.Nil {
		return 0, ErrNoSession
	}
	return uint(v), err
}

// SetEncKey stores the end-to-end AES key for a session.
func (s *Store) SetEncKey(ctx context.Context, sid string, key []byte) error {
	return s.rdb.Set(ctx, enckeyKey(sid),
		base64.StdEncoding.EncodeToString(key), s.refreshTTL).Err()
}

// EncKey returns the end-to-end AES key for a session (nil if none set).
func (s *Store) EncKey(ctx context.Context, sid string) ([]byte, error) {
	v, err := s.rdb.Get(ctx, enckeyKey(sid)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(v)
}

// UseNonce records a request nonce and returns true if it is fresh (unused).
// A repeated nonce within its TTL returns false (replay).
func (s *Store) UseNonce(ctx context.Context, sid, nonce string, ttl time.Duration) (bool, error) {
	return s.rdb.SetNX(ctx, nonceKey(sid, nonce), "1", ttl).Result()
}

// Revoke ends a session (logout) — deletes its secret and admin mapping. Any
// refresh tokens naturally become invalid (their sid no longer resolves).
func (s *Store) Revoke(ctx context.Context, sid string) error {
	return s.rdb.Del(ctx, secretKey(sid), adminKey(sid), enckeyKey(sid)).Err()
}
