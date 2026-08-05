package handler

import (
	"context"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// LandingHandler serves the aggregated public landing-page content.
type LandingHandler struct {
	nav          *repository.OrderedRepo[model.LandingNavItem]
	stats        *repository.OrderedRepo[model.LandingStat]
	features     *repository.OrderedRepo[model.LandingFeature]
	testimonials *repository.OrderedRepo[model.LandingTestimonial]
	faqs         *repository.OrderedRepo[model.LandingFaq]
	text         *repository.LandingTextRepo
	seo          *repository.LandingSeoRepo
	settings     *repository.SettingRepository
	cache        *cache.Store
}

// NewLandingHandler builds a LandingHandler.
func NewLandingHandler(
	nav *repository.OrderedRepo[model.LandingNavItem],
	stats *repository.OrderedRepo[model.LandingStat],
	features *repository.OrderedRepo[model.LandingFeature],
	testimonials *repository.OrderedRepo[model.LandingTestimonial],
	faqs *repository.OrderedRepo[model.LandingFaq],
	text *repository.LandingTextRepo,
	seo *repository.LandingSeoRepo,
	settings *repository.SettingRepository,
	c *cache.Store,
) *LandingHandler {
	return &LandingHandler{nav, stats, features, testimonials, faqs, text, seo, settings, c}
}

// landingAggregate is the cacheable static content of the landing page (every
// piece except the captcha, which is derived live from the cached settings so a
// captcha change reflects immediately). Only ENABLED nav items are stored.
type landingAggregate struct {
	Nav          []model.LandingNavItem
	Stats        []model.LandingStat
	Features     []model.LandingFeature
	Testimonials []model.LandingTestimonial
	FAQs         []model.LandingFaq
	Text         *model.LandingText
	Seo          *model.LandingSeo
}

// Public returns the whole landing page content. GET /api/v1/landing (no auth).
// Only ENABLED nav items are returned.
func (h *LandingHandler) Public(c *fiber.Ctx) error {
	// The static content (7 tables) is cached as one aggregate under
	// KeyLandingPublic (no expiry); any landing write busts it (see the repos).
	agg, err := h.aggregate()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load content")
	}

	// Captcha is derived live from the (separately cached) settings, so a captcha
	// change reflects immediately without busting the landing aggregate.
	captcha := fiber.Map{"enabled": false}
	if s, err := h.settings.Get(); err == nil && s.CaptchaEnabled &&
		s.CaptchaSiteKey != "" && s.CaptchaSecret != "" {
		captcha = fiber.Map{
			"enabled":  true,
			"provider": s.CaptchaProvider,
			"site_key": s.CaptchaSiteKey,
		}
	}

	return c.JSON(fiber.Map{
		"success":      true,
		"nav":          agg.Nav,
		"stats":        agg.Stats,
		"features":     agg.Features,
		"testimonials": agg.Testimonials,
		"faqs":         agg.FAQs,
		"text":         agg.Text,
		"seo":          agg.Seo,
		"captcha":      captcha,
	})
}

// aggregate returns the landing content, from the Redis cache when warm; a miss
// loads all seven tables and re-warms it.
func (h *LandingHandler) aggregate() (*landingAggregate, error) {
	ctx := context.Background()
	var agg landingAggregate
	if h.cache.Get(ctx, cache.KeyLandingPublic, &agg) {
		return &agg, nil
	}

	nav, err := h.nav.List()
	if err != nil {
		return nil, err
	}
	enabledNav := make([]model.LandingNavItem, 0, len(nav))
	for _, n := range nav {
		if n.Enabled {
			enabledNav = append(enabledNav, n)
		}
	}
	stats, err := h.stats.List()
	if err != nil {
		return nil, err
	}
	features, err := h.features.List()
	if err != nil {
		return nil, err
	}
	testimonials, err := h.testimonials.List()
	if err != nil {
		return nil, err
	}
	faqs, err := h.faqs.List()
	if err != nil {
		return nil, err
	}
	text, err := h.text.Get()
	if err != nil {
		return nil, err
	}
	seo, err := h.seo.Get()
	if err != nil {
		return nil, err
	}

	agg = landingAggregate{
		Nav: enabledNav, Stats: stats, Features: features,
		Testimonials: testimonials, FAQs: faqs, Text: text, Seo: seo,
	}
	h.cache.Set(ctx, cache.KeyLandingPublic, agg)
	return &agg, nil
}
