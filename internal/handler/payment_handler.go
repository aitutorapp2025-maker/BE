package handler

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
	"github.com/gofiber/fiber/v2"
)

// PaymentHandler exposes the Razorpay UPI-AutoPay endpoints: the signed-in
// student starts a subscription; Razorpay's servers call the webhook.
type PaymentHandler struct {
	payments *service.PaymentService
	log      *logger.Logger
}

// NewPaymentHandler builds a PaymentHandler.
func NewPaymentHandler(payments *service.PaymentService, log *logger.Logger) *PaymentHandler {
	return &PaymentHandler{payments: payments, log: log}
}

type subscribeRequest struct {
	PlanID uint `json:"plan_id"`
}

// Subscribe starts a UPI-AutoPay subscription and returns the checkout link.
// POST /api/v1/student/subscribe  { "plan_id": 3 }  (signed + encrypted)
func (h *PaymentHandler) Subscribe(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	if !h.payments.Enabled() {
		return fiber.NewError(fiber.StatusServiceUnavailable,
			"online payments are not configured yet")
	}
	var req subscribeRequest
	if err := c.BodyParser(&req); err != nil || req.PlanID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "a plan_id is required")
	}
	res, err := h.payments.CreateSubscription(studentID, req.PlanID)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not start the subscription: "+err.Error())
	}
	return c.JSON(fiber.Map{
		"success":         true,
		"subscription_id": res.SubscriptionID,
		"short_url":       res.ShortURL,
		"key_id":          res.KeyID,
	})
}

// MandateIntent registers a headless UPI-AutoPay mandate and returns a UPI
// intent deeplink the app launches directly (GPay) — no Razorpay checkout UI.
// POST /api/v1/student/mandate-intent  { "plan_id": 3 }  (signed + encrypted)
func (h *PaymentHandler) MandateIntent(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	if !h.payments.Enabled() {
		return fiber.NewError(fiber.StatusServiceUnavailable,
			"online payments are not configured yet")
	}
	var req subscribeRequest
	if err := c.BodyParser(&req); err != nil || req.PlanID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "a plan_id is required")
	}
	res, err := h.payments.CreateMandateIntent(studentID, req.PlanID)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "could not start autopay: "+err.Error())
	}
	return c.JSON(fiber.Map{
		"success":    true,
		"payment_id": res.PaymentID,
		"link":       res.Link,
	})
}

// Webhook receives Razorpay subscription events. It is PUBLIC and PLAINTEXT by
// necessity — Razorpay's servers call it and can't perform our E2E handshake —
// so it is authenticated by the HMAC-SHA256 signature in X-Razorpay-Signature
// instead (verified in the service). Always ACK 200 for a valid signature so
// Razorpay doesn't retry a delivery we've already recorded.
//
// POST /api/v1/payments/webhook
func (h *PaymentHandler) Webhook(c *fiber.Ctx) error {
	body := c.Body()
	sig := c.Get("X-Razorpay-Signature")
	handled, err := h.payments.HandleWebhook(body, sig)
	if !handled {
		return fiber.NewError(fiber.StatusBadRequest, "invalid signature")
	}
	if err != nil {
		// Signature was valid but processing failed — log and 200 so Razorpay
		// doesn't hammer retries; investigate from the logs.
		h.log.Errorf("razorpay webhook: %v", err)
	}
	return c.JSON(fiber.Map{"success": true})
}
