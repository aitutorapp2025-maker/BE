package handler

import (
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
	settings     *repository.SettingRepository
}

// NewLandingHandler builds a LandingHandler.
func NewLandingHandler(
	nav *repository.OrderedRepo[model.LandingNavItem],
	stats *repository.OrderedRepo[model.LandingStat],
	features *repository.OrderedRepo[model.LandingFeature],
	testimonials *repository.OrderedRepo[model.LandingTestimonial],
	faqs *repository.OrderedRepo[model.LandingFaq],
	text *repository.LandingTextRepo,
	settings *repository.SettingRepository,
) *LandingHandler {
	return &LandingHandler{nav, stats, features, testimonials, faqs, text, settings}
}

// Public returns the whole landing page content. GET /api/v1/landing (no auth).
// Only ENABLED nav items are returned.
func (h *LandingHandler) Public(c *fiber.Ctx) error {
	nav, err := h.nav.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load content")
	}
	enabledNav := make([]model.LandingNavItem, 0, len(nav))
	for _, n := range nav {
		if n.Enabled {
			enabledNav = append(enabledNav, n)
		}
	}

	stats, err := h.stats.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load content")
	}
	features, err := h.features.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load content")
	}
	testimonials, err := h.testimonials.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load content")
	}
	faqs, err := h.faqs.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load content")
	}
	text, err := h.text.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load content")
	}

	// Public captcha info (site key only) so the contact form can render it.
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
		"nav":          enabledNav,
		"stats":        stats,
		"features":     features,
		"testimonials": testimonials,
		"faqs":         faqs,
		"text":         text,
		"captcha":      captcha,
	})
}
