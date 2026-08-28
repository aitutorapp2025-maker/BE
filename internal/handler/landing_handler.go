package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

// LandingHandler serves the aggregated public landing-page content and the
// public website chat assistant.
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
	chat         *ai.Chat      // powers the website chat assistant
	rdb          *redis.Client // per-IP rate limiting for the assistant
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
	chat *ai.Chat,
	rdb *redis.Client,
) *LandingHandler {
	return &LandingHandler{nav, stats, features, testimonials, faqs, text, seo, settings, c, chat, rdb}
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

	// Captcha + contact/socials are derived live from the (separately cached)
	// settings, so a settings change reflects immediately without busting the
	// landing aggregate.
	captcha := fiber.Map{"enabled": false}
	contact := fiber.Map{}
	if s, err := h.settings.Get(); err == nil {
		if s.CaptchaEnabled && s.CaptchaSiteKey != "" && s.CaptchaSecret != "" {
			captcha = fiber.Map{
				"enabled":  true,
				"provider": s.CaptchaProvider,
				"site_key": s.CaptchaSiteKey,
			}
		}
		contact = fiber.Map{
			"whatsapp":  s.SupportWhatsApp,
			"facebook":  s.SocialFacebook,
			"instagram": s.SocialInstagram,
			"youtube":   s.SocialYoutube,
			"twitter":   s.SocialTwitter,
			"linkedin":  s.SocialLinkedin,
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
		"contact":      contact,
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

// Chat is the public website assistant: answers visitor questions about Vaha
// AI using the landing content (features, FAQs, plans text) as its knowledge.
// Anonymous + E2E-encrypted like the other public endpoints, and rate-limited
// per IP so it can't be farmed. POST /api/v1/landing/chat  {"question": "..."}
func (h *LandingHandler) Chat(c *fiber.Ctx) error {
	var req struct {
		Question string `json:"question"`
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	q := strings.TrimSpace(req.Question)
	if q == "" {
		return fiber.NewError(fiber.StatusBadRequest, "please type a question")
	}
	if len(q) > 500 {
		q = q[:500]
	}

	// Per-IP limits: 5/minute and 30/day.
	ip := c.IP()
	ctx := context.Background()
	minuteKey := "landchat:m:" + ip
	dayKey := "landchat:d:" + ip + ":" + time.Now().Format("20060102")
	nMin, _ := h.rdb.Incr(ctx, minuteKey).Result()
	if nMin == 1 {
		h.rdb.Expire(ctx, minuteKey, time.Minute)
	}
	nDay, _ := h.rdb.Incr(ctx, dayKey).Result()
	if nDay == 1 {
		h.rdb.Expire(ctx, dayKey, 25*time.Hour)
	}
	if nMin > 5 || nDay > 30 {
		return fiber.NewError(fiber.StatusTooManyRequests,
			"You're asking very fast — please wait a moment and try again.")
	}

	s, _ := h.settings.Get()
	waLine := ""
	if s != nil && strings.TrimSpace(s.SupportWhatsApp) != "" {
		waLine = " For anything you can't answer, suggest contacting us on WhatsApp at " +
			s.SupportWhatsApp + "."
	}

	// The assistant's knowledge: the live landing content.
	agg, err := h.aggregate()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "assistant unavailable")
	}
	var kb strings.Builder
	if agg.Text != nil {
		fmt.Fprintf(&kb, "About: %s — %s\n", agg.Text.HeroTitle, agg.Text.HeroSubtitle)
	}
	kb.WriteString("Features:\n")
	for _, f := range agg.Features {
		fmt.Fprintf(&kb, "- %s: %s\n", f.Title, f.Description)
	}
	kb.WriteString("FAQs:\n")
	for _, f := range agg.FAQs {
		fmt.Fprintf(&kb, "Q: %s\nA: %s\n", f.Question, f.Answer)
	}

	system := "You are the friendly website assistant for Vaha AI, an AI tutoring " +
		"app for Indian school students (classes 1-12, Tamil Nadu focus). Answer " +
		"the visitor's question briefly (2-5 sentences), warmly, and ONLY about " +
		"Vaha AI — its features, plans, pricing, how it works, and getting " +
		"started. If the question is unrelated to Vaha AI, politely steer back. " +
		"Reply in the language the visitor used." + waLine
	answer, aerr := h.chat.Complete(c.Context(),
		system, "Website knowledge:\n"+kb.String()+"\nVisitor question: "+q)
	if aerr != nil {
		fallback := "I'm having trouble answering right now. "
		if s != nil && strings.TrimSpace(s.SupportWhatsApp) != "" {
			fallback += "Please message us on WhatsApp at " + s.SupportWhatsApp + " — we'd love to help!"
		} else {
			fallback += "Please try again in a moment."
		}
		return c.JSON(fiber.Map{"success": true, "answer": fallback})
	}
	return c.JSON(fiber.Map{"success": true, "answer": strings.TrimSpace(answer)})
}
