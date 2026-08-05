package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/payment"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// RazorpayProvider reads the Razorpay keys from the DB (admin Settings), env as
// a fallback. Evaluated per call so admin changes take effect without a restart.
func RazorpayProvider(settings *repository.SettingRepository, env config.RazorpayConfig) payment.ConfigFunc {
	return func() payment.Config {
		out := payment.Config{KeyID: env.KeyID, KeySecret: env.KeySecret, WebhookSecret: env.WebhookSecret}
		s, err := settings.Get()
		if err != nil {
			return out
		}
		if v := strings.TrimSpace(s.RazorpayKeyID); v != "" {
			out.KeyID = v
		}
		if v := strings.TrimSpace(s.RazorpayKeySecret); v != "" {
			out.KeySecret = v
		}
		if v := strings.TrimSpace(s.RazorpayWebhookSecret); v != "" {
			out.WebhookSecret = v
		}
		return out
	}
}

// AutopayEnabledFunc reports whether trials require a UPI-AutoPay mandate.
type AutopayEnabledFunc func() bool

// AutopayProvider reads the admin "require autopay for trials" toggle. Evaluated
// per call so admin changes take effect without a restart; defaults to true
// (autopay required) when the setting can't be read.
func AutopayProvider(settings *repository.SettingRepository) AutopayEnabledFunc {
	return func() bool {
		s, err := settings.Get()
		if err != nil {
			return true
		}
		return s.AutopayEnabled
	}
}

// PaymentService creates UPI-AutoPay subscriptions and processes Razorpay
// webhooks, granting plan credits on each successful charge.
type PaymentService struct {
	client   *payment.Client
	cfg      payment.ConfigFunc
	students *repository.StudentRepository
	plans    *repository.PlanRepository
	credits  *CreditService
	events   *repository.PaymentEventRepository
}

// NewPaymentService builds a PaymentService.
func NewPaymentService(
	client *payment.Client,
	cfg payment.ConfigFunc,
	students *repository.StudentRepository,
	plans *repository.PlanRepository,
	credits *CreditService,
	events *repository.PaymentEventRepository,
) *PaymentService {
	return &PaymentService{client: client, cfg: cfg, students: students, plans: plans, credits: credits, events: events}
}

// Enabled reports whether Razorpay is configured.
func (s *PaymentService) Enabled() bool { return s.cfg().Enabled() }

// SubscribeResult is returned to the app to open checkout.
type SubscribeResult struct {
	SubscriptionID string `json:"subscription_id"`
	ShortURL       string `json:"short_url"` // hosted UPI-AutoPay mandate page
	KeyID          string `json:"key_id"`    // for the native Razorpay SDK, if used
}

// CreateSubscription starts a UPI-AutoPay subscription for a student + plan.
func (s *PaymentService) CreateSubscription(studentID, planID uint) (*SubscribeResult, error) {
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, fmt.Errorf("student: %w", err)
	}
	plan, err := s.plans.FindByID(planID)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	if strings.TrimSpace(plan.RazorpayPlanID) == "" {
		return nil, fmt.Errorf("this plan isn't linked to Razorpay yet")
	}
	// Schedule the first charge for the trial's end so the mandate is authorized
	// now (₹1 UPI-AutoPay confirmation) and the base plan only auto-debits after
	// the trial. If the trial has passed (or isn't set), charge immediately.
	var startAt int64
	if st.TrialEndsAt != nil && st.TrialEndsAt.After(time.Now()) {
		startAt = st.TrialEndsAt.Unix()
	}
	// Authorize 12 cycles by default; the mandate can be renewed later.
	sub, err := s.client.CreateSubscription(plan.RazorpayPlanID, 12*plan.DurationDays/30, startAt)
	if err != nil {
		return nil, err
	}
	st.RazorpaySubscriptionID = sub.ID
	if err := s.students.Update(st); err != nil {
		return nil, fmt.Errorf("save subscription: %w", err)
	}
	return &SubscribeResult{SubscriptionID: sub.ID, ShortURL: sub.ShortURL, KeyID: s.cfg().KeyID}, nil
}

// mandateAuthPaise is the small verification debit that registers the UPI-AutoPay
// mandate (₹1). The real plan amount is auto-debited later via ChargeDueMandates.
const mandateAuthPaise = 100

// MandateIntentResult is returned to the app: a UPI intent deeplink to launch the
// customer's UPI app (GPay) directly for mandate approval — no Razorpay UI.
type MandateIntentResult struct {
	PaymentID string `json:"payment_id"`
	Link      string `json:"link"`
}

// CreateMandateIntent registers a headless UPI-AutoPay mandate (Orders+Tokens
// intent flow) for a student + plan and returns the launchable GPay deeplink.
// The ₹1 authorization debit happens now; the plan amount auto-debits later
// (ChargeDueMandates) once the mandate is authorized via the webhook.
func (s *PaymentService) CreateMandateIntent(studentID, planID uint) (*MandateIntentResult, error) {
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return nil, fmt.Errorf("student: %w", err)
	}
	plan, err := s.plans.FindByID(planID)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	if plan.PriceRupees <= 0 {
		return nil, fmt.Errorf("this plan has no price to auto-debit")
	}
	// Reuse (or create) the Razorpay customer for this student.
	if strings.TrimSpace(st.RazorpayCustomerID) == "" {
		name := st.Name
		if name == "" {
			name = "Vaha AI Student"
		}
		contact := normalizeContact(st.Phone)
		custID, err := s.client.CreateCustomer(name, st.Email, contact)
		if err != nil && strings.Contains(err.Error(), "already exists") && contact != "" {
			// A customer with this phone already exists at Razorpay — usually a
			// prior attempt whose id we didn't persist, possibly with a different
			// name/email (contact is Razorpay's unique key, so fail_existing:0
			// can't reconcile that). Fetch the existing one by contact alone.
			custID, err = s.client.CreateCustomer("", "", contact)
		}
		if err != nil {
			return nil, fmt.Errorf("create customer: %w", err)
		}
		st.RazorpayCustomerID = custID
		// Persist the customer id IMMEDIATELY so a failure in the mandate steps
		// below can't lose it (which is what caused the "already exists" loop).
		if err := s.students.Update(st); err != nil {
			return nil, fmt.Errorf("save customer: %w", err)
		}
	}
	notes := map[string]string{
		"student_id": strconv.FormatUint(uint64(st.ID), 10),
		"plan_id":    strconv.FormatUint(uint64(plan.ID), 10),
		"purpose":    "mandate",
	}
	// max_amount caps each future auto-debit; the mandate is valid for 10 years.
	maxAmountPaise := plan.PriceRupees * 100
	expireAt := time.Now().AddDate(10, 0, 0).Unix()
	receipt := "mandate_" + strconv.FormatUint(uint64(st.ID), 10)
	orderID, err := s.client.CreateMandateOrder(mandateAuthPaise, maxAmountPaise, st.RazorpayCustomerID, expireAt, receipt, notes)
	if err != nil {
		return nil, fmt.Errorf("mandate order: %w", err)
	}
	intent, err := s.client.CreateUpiMandateIntent(mandateAuthPaise, orderID, st.RazorpayCustomerID, notes)
	if err != nil {
		return nil, fmt.Errorf("mandate intent: %w", err)
	}
	// Remember which plan to auto-debit and when the first real charge is due
	// (trial end, or now if the trial has passed). The scheduler only charges
	// once AutopayActive + the token are set by the authorization webhook.
	st.SubscribedPlanID = plan.ID
	next := time.Now()
	if st.TrialEndsAt != nil && st.TrialEndsAt.After(next) {
		next = *st.TrialEndsAt
	}
	st.NextChargeAt = &next
	if err := s.students.Update(st); err != nil {
		return nil, fmt.Errorf("save mandate: %w", err)
	}
	return &MandateIntentResult{PaymentID: intent.PaymentID, Link: intent.Link}, nil
}

// ChargeDueMandates triggers a recurring auto-debit for every student whose
// mandate is authorized and whose NextChargeAt has passed. Credits are granted
// on the resulting payment.captured webhook (idempotent), so this only initiates
// the debit and advances NextChargeAt to avoid re-charging on the next tick.
func (s *PaymentService) ChargeDueMandates(now time.Time) (int, error) {
	due, err := s.students.ChargeableMandates(now, 200)
	if err != nil {
		return 0, err
	}
	charged := 0
	for i := range due {
		st := &due[i]
		plan, err := s.plans.FindByID(st.SubscribedPlanID)
		if err != nil || plan.PriceRupees <= 0 {
			continue
		}
		notes := map[string]string{
			"student_id": strconv.FormatUint(uint64(st.ID), 10),
			"plan_id":    strconv.FormatUint(uint64(plan.ID), 10),
			"purpose":    "recurring",
		}
		// Apply any accrued referral reward as a discount on this bill. Razorpay
		// needs a positive amount, so we never discount below ₹1; whatever we use
		// is cleared once the charge is initiated so it isn't applied twice.
		amountRupees := plan.PriceRupees
		discount := st.ReferralRewardRupees
		if discount > 0 {
			if discount > amountRupees-1 {
				discount = amountRupees - 1
			}
			if discount < 0 {
				discount = 0
			}
			amountRupees -= discount
		}
		receipt := "cycle_" + strconv.FormatUint(uint64(st.ID), 10) + "_" + strconv.FormatInt(now.Unix(), 10)
		if _, err := s.client.ChargeRecurring(amountRupees*100, st.RazorpayCustomerID, st.RazorpayTokenID, receipt, notes); err != nil {
			continue // leave NextChargeAt so it retries next tick
		}
		// Advance the next charge by the plan's cycle length so we don't re-debit
		// before the webhook confirms this one.
		days := plan.DurationDays
		if days <= 0 {
			days = 30
		}
		next := now.AddDate(0, 0, days)
		st.NextChargeAt = &next
		// Consume the referral discount we just applied (only what we used).
		if discount > 0 {
			st.ReferralRewardRupees -= discount
		}
		_ = s.students.Update(st)
		charged++
	}
	return charged, nil
}

// normalizeContact returns a Razorpay-friendly contact: a bare 10-digit Indian
// number is prefixed with +91; anything else is passed through as-is.
func normalizeContact(phone string) string {
	p := strings.TrimSpace(phone)
	if len(p) == 10 {
		return "+91" + p
	}
	return p
}

// razorpay webhook payload (subset).
type webhookPayload struct {
	Event   string `json:"event"`
	Payload struct {
		Subscription struct {
			Entity struct {
				ID     string `json:"id"`
				PlanID string `json:"plan_id"`
			} `json:"entity"`
		} `json:"subscription"`
		Payment struct {
			Entity struct {
				ID         string            `json:"id"`
				Amount     int64             `json:"amount"`
				CustomerID string            `json:"customer_id"`
				TokenID    string            `json:"token_id"`
				Notes      map[string]string `json:"notes"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}

// HandleWebhook verifies the signature and applies the event. On
// subscription.charged it grants the plan's credits (idempotently); on
// halted/cancelled it marks the student expired. Returns (handled, error) —
// handled is false only for an invalid signature (→ 400).
func (s *PaymentService) HandleWebhook(body []byte, signature string) (bool, error) {
	if !payment.VerifyWebhook(body, signature, s.cfg().WebhookSecret) {
		return false, nil
	}
	var wh webhookPayload
	if err := json.Unmarshal(body, &wh); err != nil {
		return true, fmt.Errorf("bad webhook body: %w", err)
	}
	subID := wh.Payload.Subscription.Entity.ID

	// Headless UPI-mandate events (Orders+Tokens intent flow). These carry no
	// subscription id — we route by the notes we tagged (student_id + purpose).
	switch wh.Event {
	case "payment.authorized", "payment.captured":
		return s.handleMandatePayment(wh)
	}

	switch wh.Event {
	case "subscription.charged":
		payID := wh.Payload.Payment.Entity.ID
		amount := wh.Payload.Payment.Entity.Amount
		st, err := s.students.FindBySubscriptionID(subID)
		if err != nil {
			return true, fmt.Errorf("no student for subscription %s: %w", subID, err)
		}
		// Idempotency: skip if we've already granted for this payment.
		fresh, err := s.events.Record(payID, wh.Event, st.ID, amount)
		if err != nil {
			return true, fmt.Errorf("record event: %w", err)
		}
		if !fresh {
			return true, nil // duplicate delivery — already granted
		}
		plan, err := s.plans.FindByRazorpayPlanID(wh.Payload.Subscription.Entity.PlanID)
		if err != nil {
			return true, fmt.Errorf("no plan for %s: %w", wh.Payload.Subscription.Entity.PlanID, err)
		}
		if _, err := s.credits.Grant(int(st.ID), plan.Credits, amount, "subscription",
			"Razorpay "+payID); err != nil {
			return true, fmt.Errorf("grant credits: %w", err)
		}
		st.Plan = plan.Name
		st.PayStatus = "paid"
		st.AutopayActive = true
		_ = s.students.Update(st)
		return true, nil

	case "subscription.authenticated", "subscription.activated":
		// The mandate is set up (UPI AutoPay enabled) — the trial is now usable
		// and the base plan will auto-debit at start_at.
		if st, err := s.students.FindBySubscriptionID(subID); err == nil {
			st.AutopayActive = true
			_ = s.students.Update(st)
		}
		return true, nil

	case "subscription.halted", "subscription.cancelled", "subscription.completed":
		// Mandate deleted/stopped — autopay off. The student is prompted to
		// re-enable it; a trial without autopay can no longer be used.
		if st, err := s.students.FindBySubscriptionID(subID); err == nil {
			st.AutopayActive = false
			if st.PayStatus == "paid" {
				st.PayStatus = "expired"
			}
			_ = s.students.Update(st)
		}
		return true, nil
	}
	// Other events (activated, authenticated, pending, …) — acknowledged, no-op.
	return true, nil
}

// handleMandatePayment processes payment.authorized/captured for the headless
// UPI-mandate flow. It only acts when our notes.purpose is present (so it never
// interferes with legacy Subscriptions payments, which also emit payment.captured
// but are handled via subscription.charged).
func (s *PaymentService) handleMandatePayment(wh webhookPayload) (bool, error) {
	pay := wh.Payload.Payment.Entity
	purpose := pay.Notes["purpose"]
	if purpose != "mandate" && purpose != "recurring" {
		return true, nil // not our headless flow — no-op
	}
	// Resolve the student from the tagged id, falling back to the customer id.
	var st *model.Student
	if idStr := pay.Notes["student_id"]; idStr != "" {
		if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			st, _ = s.students.FindByID(uint(id))
		}
	}
	if st == nil {
		var err error
		if st, err = s.students.FindByCustomerID(pay.CustomerID); err != nil {
			return true, fmt.Errorf("no student for payment %s", pay.ID)
		}
	}

	if purpose == "mandate" {
		// Mandate registered — store the token and turn autopay on. (No credits
		// for the ₹1 verification debit; the plan amount is charged separately.)
		if pay.TokenID != "" {
			st.RazorpayTokenID = pay.TokenID
		}
		st.AutopayActive = true
		_ = s.students.Update(st)
		// Refund the ₹1 verification debit once it's actually captured. Record the
		// event first so a duplicate webhook delivery doesn't refund twice.
		if wh.Event == "payment.captured" {
			if fresh, _ := s.events.Record(pay.ID, wh.Event, st.ID, pay.Amount); fresh {
				if err := s.client.Refund(pay.ID, 0); err != nil {
					return true, fmt.Errorf("refund mandate ₹1 (%s): %w", pay.ID, err)
				}
			}
		}
		return true, nil
	}

	// purpose == "recurring": grant the plan's credits (idempotently) and mark paid.
	if wh.Event != "payment.captured" {
		return true, nil // wait for capture before granting
	}
	fresh, err := s.events.Record(pay.ID, wh.Event, st.ID, pay.Amount)
	if err != nil {
		return true, fmt.Errorf("record event: %w", err)
	}
	if !fresh {
		return true, nil // duplicate delivery
	}
	planID := st.SubscribedPlanID
	if idStr := pay.Notes["plan_id"]; idStr != "" {
		if id, err := strconv.ParseUint(idStr, 10, 64); err == nil {
			planID = uint(id)
		}
	}
	plan, err := s.plans.FindByID(planID)
	if err != nil {
		return true, fmt.Errorf("no plan %d: %w", planID, err)
	}
	if _, err := s.credits.Grant(int(st.ID), plan.Credits, pay.Amount, "subscription", "Razorpay "+pay.ID); err != nil {
		return true, fmt.Errorf("grant credits: %w", err)
	}
	st.Plan = plan.Name
	st.PayStatus = "paid"
	st.AutopayActive = true
	_ = s.students.Update(st)
	return true, nil
}
