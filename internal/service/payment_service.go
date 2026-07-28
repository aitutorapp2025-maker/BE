package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/config"
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
				ID     string `json:"id"`
				Amount int64  `json:"amount"`
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
		_ = s.students.Update(st)
		return true, nil

	case "subscription.halted", "subscription.cancelled", "subscription.completed":
		if st, err := s.students.FindBySubscriptionID(subID); err == nil {
			st.PayStatus = "expired"
			_ = s.students.Update(st)
		}
		return true, nil
	}
	// Other events (activated, authenticated, pending, …) — acknowledged, no-op.
	return true, nil
}
