package cache

import (
	"bytes"
	"context"
	"encoding/gob"

	"github.com/redis/go-redis/v9"
)

// Cache-key constants for the read-mostly config / master data that rarely
// changes but is read on almost every app open. Entries NEVER expire — each is
// rebuilt on the next read after a write busts it (see the repositories).
const (
	KeySettings          = "cfg:settings"           // the singleton settings row (incl. secrets)
	KeyPlans             = "cfg:plans"              // all plans, price-ordered
	KeyTeachLangsActive  = "cfg:teachlangs:active"  // active teaching languages
	KeyClassGroupsPrefix = "cfg:classgroups:active:" // + "<class>|<board>"
	KeyLandingPublic     = "cfg:landing:public"     // assembled public landing payload
)

// Store is a no-expiry Redis cache for read-mostly config/master data. Values
// are gob-encoded (NOT JSON) on purpose: gob preserves every exported field,
// including a Setting's `json:"-"` secret fields (API keys, webhook secrets)
// that JSON would silently drop. Entries never expire — callers MUST bust the
// relevant key(s) on every create/update/delete (Del / DelPattern), so the next
// read rebuilds a fresh entry. A nil Store (or nil client) is a no-op, so the
// app still works when Redis is unavailable.
type Store struct {
	rdb *redis.Client
}

// NewStore builds a Store over the given Redis client.
func NewStore(rdb *redis.Client) *Store { return &Store{rdb: rdb} }

// Get gob-decodes the value at key into dest (a pointer). Returns true on hit;
// false on a miss, decode error, or when caching is disabled.
func (s *Store) Get(ctx context.Context, key string, dest any) bool {
	if s == nil || s.rdb == nil {
		return false
	}
	b, err := s.rdb.Get(ctx, key).Bytes()
	if err != nil || len(b) == 0 {
		return false
	}
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(dest); err != nil {
		return false
	}
	return true
}

// Set gob-encodes v under key with NO expiry. Best-effort (errors are ignored —
// a cache write failure must never break the request).
func (s *Store) Set(ctx context.Context, key string, v any) {
	if s == nil || s.rdb == nil {
		return
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return
	}
	_ = s.rdb.Set(ctx, key, buf.Bytes(), 0).Err() // 0 = never expire
}

// Del removes keys — call on every write so the next read rebuilds the entry.
func (s *Store) Del(ctx context.Context, keys ...string) {
	if s == nil || s.rdb == nil || len(keys) == 0 {
		return
	}
	_ = s.rdb.Del(ctx, keys...).Err()
}

// DelPattern removes every key matching a glob (SCAN + DEL), for multi-key
// caches such as the per-(class, board) group lists.
func (s *Store) DelPattern(ctx context.Context, pattern string) {
	if s == nil || s.rdb == nil {
		return
	}
	var cursor uint64
	for {
		keys, cur, err := s.rdb.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			_ = s.rdb.Del(ctx, keys...).Err()
		}
		cursor = cur
		if cursor == 0 {
			return
		}
	}
}
