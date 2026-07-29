package handler

import (
	"html"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// identifiable is implemented by landing list models so the generic handler can
// force the id from the URL path.
type identifiable interface {
	SetID(uint)
}

// LandingCrudHandler is a generic admin CRUD handler for a landing list entity.
// PT is the *T pointer type, constrained to implement identifiable.
type LandingCrudHandler[T any, PT interface {
	*T
	identifiable
}] struct {
	repo   *repository.OrderedRepo[T]
	entity string
}

// NewLandingCrudHandler builds a generic CRUD handler for entity type T.
func NewLandingCrudHandler[T any, PT interface {
	*T
	identifiable
}](repo *repository.OrderedRepo[T], entity string) *LandingCrudHandler[T, PT] {
	return &LandingCrudHandler[T, PT]{repo: repo, entity: entity}
}

// List returns all rows. GET .../<entity>
func (h *LandingCrudHandler[T, PT]) List(c *fiber.Ctx) error {
	items, err := h.repo.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load "+h.entity)
	}
	return c.JSON(fiber.Map{"success": true, "items": items})
}

// Create inserts a new row. POST .../<entity>
func (h *LandingCrudHandler[T, PT]) Create(c *fiber.Ctx) error {
	var item T
	if err := c.BodyParser(&item); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	PT(&item).SetID(0) // server assigns the id
	if err := h.repo.Create(&item); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create "+h.entity)
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "item": item})
}

// Update edits a row. PUT .../<entity>/:id
func (h *LandingCrudHandler[T, PT]) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	// Load existing so unset fields (e.g. created_at) are preserved.
	existing, err := h.repo.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, h.entity)
	}
	if err := c.BodyParser(existing); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	PT(existing).SetID(id) // the path id always wins
	if err := h.repo.Update(existing); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update "+h.entity)
	}
	return c.JSON(fiber.Map{"success": true, "item": existing})
}

// Delete removes a row. DELETE .../<entity>/:id
func (h *LandingCrudHandler[T, PT]) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.repo.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete "+h.entity)
	}
	return c.JSON(fiber.Map{"success": true})
}

// LandingTextHandler handles the singleton landing text (GET/PUT).
type LandingTextHandler struct {
	repo *repository.LandingTextRepo
}

// NewLandingTextHandler builds a LandingTextHandler.
func NewLandingTextHandler(repo *repository.LandingTextRepo) *LandingTextHandler {
	return &LandingTextHandler{repo: repo}
}

// Get returns the landing text. GET /api/v1/admin/landing/text
func (h *LandingTextHandler) Get(c *fiber.Ctx) error {
	t, err := h.repo.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load text")
	}
	return c.JSON(fiber.Map{"success": true, "text": t})
}

// Update saves the landing text. PUT /api/v1/admin/landing/text
func (h *LandingTextHandler) Update(c *fiber.Ctx) error {
	existing, err := h.repo.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load text")
	}
	if err := c.BodyParser(existing); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	existing.ID = 1
	if err := h.repo.Save(existing); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to save text")
	}
	return c.JSON(fiber.Map{"success": true, "text": existing})
}

// LandingSeoHandler handles the singleton landing SEO / meta (GET/PUT).
type LandingSeoHandler struct {
	repo *repository.LandingSeoRepo
}

// NewLandingSeoHandler builds a LandingSeoHandler.
func NewLandingSeoHandler(repo *repository.LandingSeoRepo) *LandingSeoHandler {
	return &LandingSeoHandler{repo: repo}
}

// Get returns the landing SEO meta. GET /api/v1/admin/landing/seo
func (h *LandingSeoHandler) Get(c *fiber.Ctx) error {
	s, err := h.repo.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load SEO")
	}
	return c.JSON(fiber.Map{"success": true, "seo": s})
}

// Update saves the landing SEO meta. PUT /api/v1/admin/landing/seo
func (h *LandingSeoHandler) Update(c *fiber.Ctx) error {
	existing, err := h.repo.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load SEO")
	}
	if err := c.BodyParser(existing); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	existing.ID = 1
	if err := h.repo.Save(existing); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to save SEO")
	}
	return c.JSON(fiber.Map{"success": true, "seo": existing})
}

// MetaHTML renders the landing SEO as a ready-to-inject HTML <head> fragment
// (title + meta/link tags). It is PLAIN (not E2E-encrypted) so a hosting layer
// (nginx SSI, a prerender service) can fetch it and inject the real tags into
// index.html for social crawlers that don't run JavaScript.
// GET /api/v1/landing/meta.html
func (h *LandingSeoHandler) MetaHTML(c *fiber.Ctx) error {
	s, err := h.repo.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load SEO")
	}

	domain := strings.TrimRight(s.SiteDomain, "/")
	canonical := seoFirst(s.CanonicalURL, s.SiteDomain)
	ogTitle := seoFirst(s.OgTitle, s.MetaTitle)
	ogDesc := seoFirst(s.OgDescription, s.MetaDescription)
	ogImage := seoResolveImage(s.OgImage, domain)
	twImage := seoResolveImage(seoFirst(s.TwitterImage, s.OgImage), domain)

	var b strings.Builder
	if s.MetaTitle != "" {
		b.WriteString("<title>" + html.EscapeString(s.MetaTitle) + "</title>\n")
	}
	seoMeta(&b, "name", "description", s.MetaDescription)
	seoMeta(&b, "name", "keywords", s.MetaKeywords)
	seoMeta(&b, "name", "robots", s.Robots)
	seoMeta(&b, "name", "theme-color", s.ThemeColor)
	if canonical != "" {
		b.WriteString(`<link rel="canonical" href="` + html.EscapeString(canonical) + "\">\n")
	}
	seoMeta(&b, "property", "og:title", ogTitle)
	seoMeta(&b, "property", "og:description", ogDesc)
	seoMeta(&b, "property", "og:type", seoFirst(s.OgType, "website"))
	seoMeta(&b, "property", "og:image", ogImage)
	seoMeta(&b, "property", "og:url", seoFirst(s.OgURL, canonical))
	seoMeta(&b, "property", "og:site_name", s.OgSiteName)
	seoMeta(&b, "name", "twitter:card", seoFirst(s.TwitterCard, "summary_large_image"))
	seoMeta(&b, "name", "twitter:site", s.TwitterSite)
	seoMeta(&b, "name", "twitter:title", seoFirst(s.TwitterTitle, ogTitle))
	seoMeta(&b, "name", "twitter:description", seoFirst(s.TwitterDescription, ogDesc))
	seoMeta(&b, "name", "twitter:image", twImage)
	if s.StructuredData != "" {
		b.WriteString(`<script type="application/ld+json" id="seo-jsonld">` +
			s.StructuredData + "</script>\n")
	}

	c.Set("Cache-Control", "public, max-age=300")
	return c.Type("html", "utf-8").SendString(b.String())
}

// seoFirst returns the first non-empty (trimmed) value.
func seoFirst(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// seoResolveImage returns an absolute image URL: absolute URLs are kept as-is;
// a root-relative path ("/og.png") is joined onto the domain when one is set.
func seoResolveImage(img, domain string) string {
	img = strings.TrimSpace(img)
	if img == "" {
		return ""
	}
	if strings.HasPrefix(img, "http://") || strings.HasPrefix(img, "https://") {
		return img
	}
	if domain != "" && strings.HasPrefix(img, "/") {
		return domain + img
	}
	return img
}

// seoMeta appends a <meta {attr}="{key}" content="{value}"> line when value is
// non-empty, escaping the value.
func seoMeta(b *strings.Builder, attr, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString("<meta " + attr + `="` + key + `" content="` +
		html.EscapeString(value) + "\">\n")
}

// Compile-time assertions that the models satisfy identifiable via pointers.
var (
	_ identifiable = (*model.LandingNavItem)(nil)
	_ identifiable = (*model.LandingStat)(nil)
	_ identifiable = (*model.LandingFeature)(nil)
	_ identifiable = (*model.LandingTestimonial)(nil)
	_ identifiable = (*model.LandingFaq)(nil)
)
