package handler

import (
	"net/url"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// ReferralHandler exposes the student-facing referral endpoint and the admin
// referral list.
type ReferralHandler struct {
	referral  *service.ReferralService
	referrals *repository.ReferralRepository
}

// NewReferralHandler builds a ReferralHandler.
func NewReferralHandler(referral *service.ReferralService, referrals *repository.ReferralRepository) *ReferralHandler {
	return &ReferralHandler{referral: referral, referrals: referrals}
}

// Me returns the signed-in student's referral info (code, share link, message,
// reward + count). Generates the code on first access.
//
// GET /api/v1/student/referral  (Bearer student JWT)
func (h *ReferralHandler) Me(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	info, err := h.referral.MyReferral(studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load your referral details")
	}
	return c.JSON(fiber.Map{"success": true, "referral": info})
}

// List returns recent referral attributions for the admin panel.
//
// GET /api/v1/admin/referrals
func (h *ReferralHandler) List(c *fiber.Ctx) error {
	rows, err := h.referrals.ListRecent(200)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load referrals")
	}
	return c.JSON(fiber.Map{"success": true, "referrals": rows})
}

// ReferralRedirectHandler serves the public short link (/r/:code). It detects the
// visitor's platform from the User-Agent and 302-redirects to the correct app
// store — Android → Play Store (with the referral code as a Play install-referrer
// so the code survives install), iPhone/iPad → App Store, anything else → the
// Play Store as a sensible default. Public + plaintext (shared over WhatsApp), so
// it does NOT go through the E2E middleware.
type ReferralRedirectHandler struct {
	settings *repository.SettingRepository
}

// NewReferralRedirectHandler builds a ReferralRedirectHandler.
func NewReferralRedirectHandler(settings *repository.SettingRepository) *ReferralRedirectHandler {
	return &ReferralRedirectHandler{settings: settings}
}

// Redirect sends the visitor to the appropriate app store for their device.
func (h *ReferralRedirectHandler) Redirect(c *fiber.Ctx) error {
	code := strings.ToUpper(strings.TrimSpace(c.Params("code")))
	set, err := h.settings.Get()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "unavailable")
	}

	ua := strings.ToLower(string(c.Request().Header.UserAgent()))
	android := strings.TrimSpace(set.AndroidStoreURL)
	ios := strings.TrimSpace(set.IosStoreURL)

	// iPhone/iPad → App Store (when set); everyone else → Play Store, with the
	// referral code attached as a Play install-referrer. If the platform's own
	// store URL is missing, fall back to whichever one is configured.
	var target string
	switch {
	case isIOSUA(ua) && ios != "":
		target = ios
	case android != "":
		target = appendPlayReferrer(android, code)
	default:
		target = ios
	}

	if target == "" {
		// No store URLs configured yet — show a minimal message instead of a 500.
		return c.Status(fiber.StatusOK).SendString(
			"Thanks for your interest! The app link isn't set up yet — please check back soon.")
	}
	return c.Redirect(target, fiber.StatusFound)
}

// isIOSUA reports whether the User-Agent looks like an Apple mobile device.
func isIOSUA(ua string) bool {
	return strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod")
}

// appendPlayReferrer adds a Play Store install-referrer carrying the referral
// code, so the code can be recovered after install via the Play Install Referrer
// API. If the URL already has a referrer it's left untouched.
func appendPlayReferrer(storeURL, code string) string {
	if code == "" || strings.Contains(storeURL, "referrer=") {
		return storeURL
	}
	ref := url.QueryEscape("utm_source=referral&code=" + code)
	sep := "?"
	if strings.Contains(storeURL, "?") {
		sep = "&"
	}
	return storeURL + sep + "referrer=" + ref
}
