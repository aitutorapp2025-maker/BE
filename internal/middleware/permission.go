package middleware

import (
	"strings"
	"sync"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// AdminPermission enforces the role-based permission catalog on every admin
// route. It runs AFTER SignedAdmin (which sets admin_id) and resolves the
// admin's role per request — so a permission change in the panel takes effect
// immediately, no re-login needed. Lookups are cached briefly in memory to
// keep the per-request cost negligible.
func AdminPermission(admins *repository.AdminRepository) fiber.Handler {
	cache := &permCache{entries: map[uint]permEntry{}}

	return func(c *fiber.Ctx) error {
		adminID, _ := c.Locals("admin_id").(uint)
		if adminID == 0 {
			return fiber.NewError(fiber.StatusUnauthorized, "unauthorized")
		}

		// Which permissions allow this route (any-of; empty = all admins).
		_, seg, ok := strings.Cut(c.Path(), "/admin/")
		if !ok {
			return c.Next()
		}
		required := model.RequiredPermsForAdminRoute(c.Method(), seg)
		if len(required) == 0 {
			return c.Next()
		}

		admin, err := cache.get(adminID, admins)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "account not found")
		}
		if !admin.HasPerm(required) {
			return fiber.NewError(fiber.StatusForbidden,
				"you don't have permission for this section")
		}
		return c.Next()
	}
}

// permCache is a tiny TTL cache of admin→role lookups (admin traffic is low;
// this only spares a couple of PK lookups per request).
type permCache struct {
	mu      sync.Mutex
	entries map[uint]permEntry
}

type permEntry struct {
	admin   *model.Admin
	expires time.Time
}

const permCacheTTL = 15 * time.Second

func (p *permCache) get(id uint, admins *repository.AdminRepository) (*model.Admin, error) {
	p.mu.Lock()
	e, ok := p.entries[id]
	p.mu.Unlock()
	if ok && time.Now().Before(e.expires) {
		return e.admin, nil
	}
	admin, err := admins.FindByID(id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.entries[id] = permEntry{admin: admin, expires: time.Now().Add(permCacheTTL)}
	p.mu.Unlock()
	return admin, nil
}
