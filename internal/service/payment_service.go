package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/payment"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// RazorpayProvider reads the Razorpay keys from the DB (admin Settings), env as
// a fallback. Evaluated per call so admin changes take effect without a restart.
// The admin "Test mode" toggle (RazorpayTestMode) selects which stored key set
// is active — both live and test credentials stay saved.
func RazorpayProvider(settings *repository.SettingRepository, env config.RazorpayConfig) payment.ConfigFunc {
	return func() payment.Config {
		out := payment.Config{KeyID: env.KeyID, KeySecret: env.KeySecret, WebhookSecret: env.WebhookSecret}
		s, err := settings.Get()
		if err != nil {
			return out
		}
		keyID, keySecret, webhook := s.RazorpayKeyID, s.RazorpayKeySecret, s.RazorpayWebhookSecret
		if s.RazorpayTestMode {
			keyID, keySecret, webhook = s.RazorpayTestKeyID, s.RazorpayTestKeySecret, s.RazorpayTestWebhookSecret
		}
		if v := strings.TrimSpace(keyID); v != "" {
			out.KeyID = v
		}
		if v := strings.TrimSpace(keySecret); v != "" {
			out.KeySecret = v
		}
		if v := strings.TrimSpace(webhook); v != "" {
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

// ProfilePasswordProvider reads the admin "profile password lock" toggle
// (defaults off). Reuses the AutopayEnabledFunc (func() bool) shape.
func ProfilePasswordProvider(settings *repository.SettingRepository) AutopayEnabledFunc {
	return func() bool {
		s, err := settings.Get()
		if err != nil {
			return false
		}
		return s.ProfilePasswordEnabled
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

	// reconcileAt throttles per-student Razorpay status polls (ReconcileAutopay
	// is called from /student/me while a mandate looks pending).
	reconcileMu sync.Mutex
	reconcileAt map[uint]time.Time
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
	return &PaymentService{client: client, cfg: cfg, students: students, plans: plans,
		credits: credits, events: events, reconcileAt: map[uint]time.Time{}}
}

// ReconcileAutopay checks the student's Razorpay subscription directly and
// marks the mandate active if Razorpay says it is authenticated/active. This
// is the fallback for a MISSED webhook (backend not publicly reachable in dev,
// or a delivery failure in prod): the app polls /student/me while it shows the
// "set up autopay" prompt, so a completed mandate heals within one throttle
// window instead of sticking forever. Returns true if the flag was flipped.
// Throttled to one Razorpay call per student per 30s.
func (s *PaymentService) ReconcileAutopay(st *model.Student) bool {
	if st == nil || st.AutopayActive || strings.TrimSpace(st.RazorpaySubscriptionID) == "" || !s.Enabled() {
		return false
	}
	s.reconcileMu.Lock()
	if t, ok := s.reconcileAt[st.ID]; ok && time.Since(t) < 30*time.Second {
		s.reconcileMu.Unlock()
		return false
	}
	s.reconcileAt[st.ID] = time.Now()
	s.reconcileMu.Unlock()

	sub, err := s.client.FetchSubscription(st.RazorpaySubscriptionID)
	if err != nil {
		return false
	}
	switch sub.Status {
	case "authenticated", "active":
		st.AutopayActive = true
		if err := s.students.Update(st); err != nil {
			return false
		}
		// Same Billing row the webhook writes on first activation.
		_, _ = s.credits.Grant(int(st.ID), 0, 0, "autopay_setup",
			"AutoPay setup confirmation (up to ₹5, refunded by Razorpay)")
		return true
	}
	return false
}

// Enabled reports whether Razorpay is configured.
func (s *PaymentService) Enabled() bool { return s.cfg().Enabled() }

// CancelAutopay cancels a student's Razorpay AutoPay subscription (so it can't
// keep charging them) — called when the student deletes their account. Best
// effort: returns any Razorpay error, but the caller proceeds with deletion.
func (s *PaymentService) CancelAutopay(studentID uint) error {
	st, err := s.students.FindByID(studentID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(st.RazorpaySubscriptionID) == "" {
		return nil // no subscription to cancel
	}
	if err := s.client.CancelSubscription(st.RazorpaySubscriptionID); err != nil {
		return err
	}
	st.AutopayActive = false
	st.RazorpaySubscriptionID = ""
	return s.students.Update(st)
}

// SubscribeResult is returned to the app to open checkout.
type SubscribeResult struct {
	SubscriptionID string `json:"subscription_id"`
	ShortURL       string `json:"short_url"` // hosted UPI-AutoPay mandate page
	KeyID          string `json:"key_id"`    // for the native Razorpay SDK, if used
	// AlreadyActive means the student's existing mandate is authenticated —
	// there is nothing to pay; the app should skip checkout and refresh.
	AlreadyActive bool `json:"already_active"`
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
	// Razorpay plan ids are mode-scoped (live vs test) — use the id matching
	// the active keys, creating it on the fly when this plan has never been
	// used in this mode (e.g. right after the admin flips the Live/Test
	// toggle for tiers that predate it).
	testMode := s.cfg().IsTest()
	rzpPlanID := strings.TrimSpace(plan.RzpPlanID(testMode))
	if rzpPlanID == "" {
		id, err := s.client.CreatePlan(plan.Name, plan.PriceRupees*100, payment.IntervalMonths(plan.DurationDays))
		if err != nil {
			return nil, fmt.Errorf("this plan isn't linked to Razorpay yet (auto-create failed: %w)", err)
		}
		plan.SetRzpPlanID(testMode, id)
		if err := s.plans.Update(plan); err != nil {
			return nil, fmt.Errorf("save razorpay plan link: %w", err)
		}
		rzpPlanID = id
	}
	// Handle an existing subscription CAREFULLY — a blind cancel-before-create
	// once killed a mandate the student had just paid (the webhook announcing
	// it was missed, the app kept prompting, and the retry cancelled the good
	// subscription while its ₹5 auth was in flight):
	//   - authenticated/active on the SAME plan → nothing to pay; mark the
	//     mandate active and tell the app to skip checkout.
	//   - still awaiting payment on the SAME plan → reuse it (Razorpay allows
	//     retrying checkout on the same subscription).
	//   - dead (cancelled/expired/completed) or a DIFFERENT plan → cancel
	//     best-effort and create a fresh one below.
	if old := strings.TrimSpace(st.RazorpaySubscriptionID); old != "" {
		if sub, err := s.client.FetchSubscription(old); err == nil && sub.PlanID == rzpPlanID {
			switch sub.Status {
			case "authenticated", "active":
				if !st.AutopayActive {
					st.AutopayActive = true
					if err := s.students.Update(st); err != nil {
						return nil, fmt.Errorf("save mandate state: %w", err)
					}
					_, _ = s.credits.Grant(int(st.ID), 0, 0, "autopay_setup",
						"AutoPay setup confirmation (up to ₹5, refunded by Razorpay)")
				}
				return &SubscribeResult{SubscriptionID: old, KeyID: s.cfg().KeyID, AlreadyActive: true}, nil
			case "created", "pending":
				return &SubscribeResult{SubscriptionID: old, ShortURL: sub.ShortURL, KeyID: s.cfg().KeyID}, nil
			}
		}
		_ = s.client.CancelSubscription(old)
		st.RazorpaySubscriptionID = ""
	}
	// Setup must only CONFIRM the mandate (a small UPI-AutoPay ₹1-style
	// confirmation) and register it for the plan amount — it must NEVER charge
	// the full plan amount up front. So the first real plan debit is always
	// deferred to a future start_at: the trial's end, or (if the trial has
	// already passed) the next day. The plan amount then auto-debits from there.
	now := time.Now()
	startAt := now.Add(24 * time.Hour).Unix()
	if st.TrialEndsAt != nil && st.TrialEndsAt.After(now) {
		startAt = st.TrialEndsAt.Unix()
	}
	// Authorize 12 cycles by default; the mandate can be renewed later.
	sub, err := s.client.CreateSubscription(rzpPlanID, 12*plan.DurationDays/30, startAt)
	if err != nil && isStaleRzpID(err) {
		// The stored mode-scoped plan id doesn't exist under the CURRENT keys.
		// Razorpay ids are account-scoped, so this happens when the admin swaps
		// the Razorpay account/keys (a plain key rotation keeps ids valid, a
		// new account does not). Mint a fresh plan under the active keys, save
		// it, and retry once — same self-heal philosophy as a price change.
		id, cerr := s.client.CreatePlan(
			plan.Name, plan.PriceRupees*100, payment.IntervalMonths(plan.DurationDays))
		if cerr != nil {
			return nil, fmt.Errorf("razorpay plan re-link failed: %w (original: %v)", cerr, err)
		}
		plan.SetRzpPlanID(testMode, id)
		if serr := s.plans.Update(plan); serr != nil {
			return nil, fmt.Errorf("save razorpay plan link: %w", serr)
		}
		rzpPlanID = id
		sub, err = s.client.CreateSubscription(rzpPlanID, 12*plan.DurationDays/30, startAt)
	}
	if err != nil {
		return nil, err
	}
	st.RazorpaySubscriptionID = sub.ID
	if err := s.students.Update(st); err != nil {
		return nil, fmt.Errorf("save subscription: %w", err)
	}
	return &SubscribeResult{SubscriptionID: sub.ID, ShortURL: sub.ShortURL, KeyID: s.cfg().KeyID}, nil
}

// isStaleRzpID reports whether a Razorpay error means "this id doesn't exist
// under the current keys" — the signature of an account/key switch (BAD_REQUEST
// "The ID provided is invalid or could not be found").
func isStaleRzpID(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "razorpay status 400") &&
		(strings.Contains(msg, "could not be found") || strings.Contains(msg, "is invalid"))
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
			// A customer with this phone already exists at Razorpay — an earlier
			// attempt whose id we didn't persist. fail_existing:0 only returns it
			// when the contact format matches exactly, which an orphaned record may
			// not — so look it up by contact (matched on the last 10 digits).
			if found, ferr := s.client.FetchCustomerByContact(contact); ferr == nil && found != "" {
				custID, err = found, nil
			}
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
		// Monthly renewal: unused credits don't carry over — expire the old
		// balance to 0, then grant this cycle's fresh credits.
		_, _ = s.credits.ExpireCredits(st.ID, "Monthly reset")
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
			wasActive := st.AutopayActive
			st.AutopayActive = true
			_ = s.students.Update(st)
			// Record the AutoPay setup in Billing (once, on first activation).
			// On the hosted Subscriptions flow Razorpay charges its own small
			// authorization amount (up to ₹5) and auto-refunds it, so record
			// zero revenue — the note explains what the student saw at setup.
			if !wasActive {
				_, _ = s.credits.Grant(int(st.ID), 0, 0, "autopay_setup",
					"AutoPay setup confirmation (up to ₹5, refunded by Razorpay)")
			}
		}
		return true, nil

	case "subscription.pending":
		// A scheduled auto-debit failed (Razorpay will retry) — record it so the
		// student sees the failed charge in Billing.
		if st, err := s.students.FindBySubscriptionID(subID); err == nil {
			_, _ = s.credits.Grant(int(st.ID), 0, 0, "autopay_failed",
				"AutoPay charge failed")
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
	// Monthly renewal: expire any unused credits before granting this cycle's.
	_, _ = s.credits.ExpireCredits(st.ID, "Monthly reset")
	if _, err := s.credits.Grant(int(st.ID), plan.Credits, pay.Amount, "subscription", "Razorpay "+pay.ID); err != nil {
		return true, fmt.Errorf("grant credits: %w", err)
	}
	st.Plan = plan.Name
	st.PayStatus = "paid"
	st.AutopayActive = true
	_ = s.students.Update(st)
	return true, nil
}
