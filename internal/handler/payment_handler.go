package handler

import (
	"strconv"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
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

// Invoice returns the Razorpay details for one of the student's own payments,
// so the client can build a richer invoice (payer email/phone, UPI VPA, bank
// RRN, exact method + settlement time). Access-checked to this student.
// GET /api/v1/student/payments/:txn/invoice  (Bearer student JWT)
func (h *PaymentHandler) Invoice(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	txn := strings.TrimSpace(c.Params("txn"))
	if txn == "" || !strings.HasPrefix(txn, "pay_") {
		return fiber.NewError(fiber.StatusBadRequest, "invalid transaction id")
	}
	d, err := h.payments.FetchInvoiceDetails(studentID, txn)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "invoice details unavailable")
	}
	return c.JSON(fiber.Map{
		"success":       true,
		"id":            d.ID,
		"amount_rupees": d.Amount / 100,
		"method":        d.Method,
		"email":         d.Email,
		"contact":       d.Contact,
		"vpa":           d.Vpa,
		"rrn":           d.Acquirer.Rrn,
		"upi_txn":       d.Acquirer.UpiTransactionID,
		"status":        d.Status,
		"created_at":    d.CreatedAt,
	})
}

// AdminInvoice returns the Razorpay details for a given student's payment so the
// admin panel can build the same enriched invoice as the student app. The
// student id comes from the path (admin-authorized); FetchInvoiceDetails still
// verifies the txn belongs to that student.
// GET /api/v1/admin/students/:id/payments/:txn/invoice  (admin JWT)
func (h *PaymentHandler) AdminInvoice(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Params("id")), 10, 64)
	if err != nil || id == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid student id")
	}
	txn := strings.TrimSpace(c.Params("txn"))
	if txn == "" || !strings.HasPrefix(txn, "pay_") {
		return fiber.NewError(fiber.StatusBadRequest, "invalid transaction id")
	}
	d, err := h.payments.FetchInvoiceDetails(uint(id), txn)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "invoice details unavailable")
	}
	return c.JSON(fiber.Map{
		"success":       true,
		"id":            d.ID,
		"amount_rupees": d.Amount / 100,
		"method":        d.Method,
		"email":         d.Email,
		"contact":       d.Contact,
		"vpa":           d.Vpa,
		"rrn":           d.Acquirer.Rrn,
		"upi_txn":       d.Acquirer.UpiTransactionID,
		"status":        d.Status,
		"created_at":    d.CreatedAt,
	})
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

// AdminStudentPaymentsHandler serves the admin's per-student payment view:
// the AutoPay/mandate status plus every processed Razorpay event (with its
// payment id and amount) — mandate charge, refunds, renewals.
type AdminStudentPaymentsHandler struct {
	students *repository.StudentRepository
	events   *repository.PaymentEventRepository
}

// NewAdminStudentPaymentsHandler builds an AdminStudentPaymentsHandler.
func NewAdminStudentPaymentsHandler(students *repository.StudentRepository,
	events *repository.PaymentEventRepository) *AdminStudentPaymentsHandler {
	return &AdminStudentPaymentsHandler{students: students, events: events}
}

// Get returns one student's autopay + payment history.
// GET /api/v1/admin/students/:id/payments
func (h *AdminStudentPaymentsHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	st, err := h.students.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "student")
	}
	events, err := h.events.ListForStudent(st.ID, 200)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load payment events")
	}
	return c.JSON(fiber.Map{
		"success": true,
		"autopay": fiber.Map{
			"active":               st.AutopayActive,
			"plan":                 st.Plan,
			"pay_status":           st.PayStatus,
			"trial_ends_at":        st.TrialEndsAt,
			"razorpay_customer_id": st.RazorpayCustomerID,
			"razorpay_token_id":    st.RazorpayTokenID,
		},
		"events": events,
	})
}
