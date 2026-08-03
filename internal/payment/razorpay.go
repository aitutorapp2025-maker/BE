// Package payment integrates Razorpay UPI-AutoPay subscriptions: creating a
// subscription (server-side) and verifying the webhook signature. Called over
// net/http so the service takes on no new module dependencies.
package payment

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	subscriptionsURL   = "https://api.razorpay.com/v1/subscriptions"
	plansURL           = "https://api.razorpay.com/v1/plans"
	customersURL       = "https://api.razorpay.com/v1/customers"
	ordersURL          = "https://api.razorpay.com/v1/orders"
	createUpiURL       = "https://api.razorpay.com/v1/payments/create/upi"
	createRecurringURL = "https://api.razorpay.com/v1/payments/create/recurring"
)

// Config holds the Razorpay credentials (from admin Settings). key_id is safe to
// expose to the client (it's used to open checkout); the key secret and webhook
// secret are server-only.
type Config struct {
	KeyID         string
	KeySecret     string
	WebhookSecret string
}

// Enabled reports whether Razorpay is configured for creating subscriptions.
func (c Config) Enabled() bool { return c.KeyID != "" && c.KeySecret != "" }

// ConfigFunc returns the current Razorpay config, evaluated per call so admin
// changes take effect without a restart.
type ConfigFunc func() Config

// Client talks to the Razorpay subscriptions API.
type Client struct {
	cfg  ConfigFunc
	http *http.Client
}

// NewClient builds a Razorpay client that reads its keys from cfg per call.
func NewClient(cfg ConfigFunc) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

type createSubReq struct {
	PlanID         string `json:"plan_id"`
	TotalCount     int    `json:"total_count"`
	CustomerNotify int    `json:"customer_notify"`
	// StartAt (unix seconds) delays the first real charge until the trial ends,
	// so the mandate is authorized now (₹1 UPI-AutoPay confirmation) but the base
	// plan only auto-debits after the trial. Omitted when 0.
	StartAt int64 `json:"start_at,omitempty"`
}

// Subscription is the subset of the Razorpay response we use.
type Subscription struct {
	ID       string `json:"id"`
	ShortURL string `json:"short_url"`
	Status   string `json:"status"`
}

// CreateSubscription creates a recurring UPI-AutoPay subscription for a plan.
// totalCount is how many billing cycles to authorize (e.g. 12 for a year of
// monthly). startAt (unix seconds, 0 = now) delays the first charge to the trial
// end. Returns the subscription id + the hosted checkout short URL.
func (c *Client) CreateSubscription(planID string, totalCount int, startAt int64) (*Subscription, error) {
	cfg := c.cfg()
	if !cfg.Enabled() {
		return nil, fmt.Errorf("razorpay is not configured (set the keys in admin Settings)")
	}
	if totalCount <= 0 {
		totalCount = 12
	}
	body, _ := json.Marshal(createSubReq{PlanID: planID, TotalCount: totalCount, CustomerNotify: 1, StartAt: startAt})
	req, err := http.NewRequest(http.MethodPost, subscriptionsURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(cfg.KeyID, cfg.KeySecret)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("razorpay request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay status %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	var sub Subscription
	if err := json.Unmarshal(raw, &sub); err != nil {
		return nil, fmt.Errorf("razorpay decode: %w", err)
	}
	return &sub, nil
}

type createPlanReq struct {
	Period   string   `json:"period"`   // "monthly"
	Interval int      `json:"interval"` // months per cycle
	Item     planItem `json:"item"`
}

type planItem struct {
	Name     string `json:"name"`
	Amount   int    `json:"amount"` // in paise
	Currency string `json:"currency"`
}

// CreatePlan creates a Razorpay subscription plan and returns its id (plan_…).
// amountPaise is the per-cycle price in paise; intervalMonths is how many months
// each cycle spans (1 = monthly). Razorpay plans are immutable, so a price/period
// change means creating a new plan (a new id), not editing the old one.
func (c *Client) CreatePlan(name string, amountPaise, intervalMonths int) (string, error) {
	cfg := c.cfg()
	if !cfg.Enabled() {
		return "", fmt.Errorf("razorpay is not configured (set the keys in admin Settings)")
	}
	if intervalMonths <= 0 {
		intervalMonths = 1
	}
	if amountPaise <= 0 {
		return "", fmt.Errorf("razorpay plan needs a positive amount")
	}
	body, _ := json.Marshal(createPlanReq{
		Period:   "monthly",
		Interval: intervalMonths,
		Item:     planItem{Name: name, Amount: amountPaise, Currency: "INR"},
	})
	req, err := http.NewRequest(http.MethodPost, plansURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(cfg.KeyID, cfg.KeySecret)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("razorpay request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("razorpay status %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("razorpay decode: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("razorpay: empty plan id in response")
	}
	return out.ID, nil
}

// post sends a JSON body to a Razorpay endpoint with basic auth and decodes the
// JSON response into out. Returns an error (with a truncated body) on non-200.
func (c *Client) post(url string, body any, out any) error {
	cfg := c.cfg()
	if !cfg.Enabled() {
		return fmt.Errorf("razorpay is not configured (set the keys in admin Settings)")
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(cfg.KeyID, cfg.KeySecret)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("razorpay request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("razorpay status %d: %s", resp.StatusCode, truncate(respBody, 300))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("razorpay decode: %w", err)
		}
	}
	return nil
}

// CreateCustomer creates (or returns the existing) Razorpay customer for a
// UPI-AutoPay mandate. fail_existing=0 makes Razorpay return the existing
// customer instead of erroring when the contact/email is already on file.
func (c *Client) CreateCustomer(name, email, contact string) (string, error) {
	body := map[string]any{"fail_existing": 0}
	if name != "" {
		body["name"] = name
	}
	if email != "" {
		body["email"] = email
	}
	if contact != "" {
		body["contact"] = contact
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.post(customersURL, body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("razorpay: empty customer id")
	}
	return out.ID, nil
}

// CreateMandateOrder creates the authorization order that registers a UPI-AutoPay
// mandate. amountPaise is the registration debit (e.g. ₹1). maxAmountPaise caps
// each future auto-debit; expireAt (unix) is the mandate expiry. frequency
// "as_presented" lets us debit whenever we present a charge (up to maxAmount).
// notes are echoed back on the resulting payment webhook (we tag the student).
func (c *Client) CreateMandateOrder(amountPaise, maxAmountPaise int, customerID string, expireAt int64, receipt string, notes map[string]string) (string, error) {
	body := map[string]any{
		"amount":      amountPaise,
		"currency":    "INR",
		"customer_id": customerID,
		"method":      "upi",
		"receipt":     receipt,
		"token": map[string]any{
			"max_amount": maxAmountPaise,
			"expire_at":  expireAt,
			"frequency":  "as_presented",
		},
	}
	if len(notes) > 0 {
		body["notes"] = notes
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.post(ordersURL, body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("razorpay: empty order id")
	}
	return out.ID, nil
}

// UpiIntent is the create/upi intent response we use.
type UpiIntent struct {
	PaymentID string `json:"razorpay_payment_id"`
	// Link is the UPI intent deeplink to launch the customer's UPI app (GPay) for
	// mandate approval. Do NOT modify it — Razorpay rejects tampered links.
	Link string `json:"link"`
}

// CreateUpiMandateIntent starts the mandate authorization payment via the UPI
// INTENT flow and returns the launchable UPI deeplink. notes tag the student for
// the webhook.
func (c *Client) CreateUpiMandateIntent(amountPaise int, orderID, customerID string, notes map[string]string) (*UpiIntent, error) {
	body := map[string]any{
		"amount":      amountPaise,
		"currency":    "INR",
		"order_id":    orderID,
		"customer_id": customerID,
		"recurring":   "1",
		"method":      "upi",
		"upi":         map[string]any{"flow": "intent"},
	}
	if len(notes) > 0 {
		body["notes"] = notes
	}
	var out UpiIntent
	if err := c.post(createUpiURL, body, &out); err != nil {
		return nil, err
	}
	if out.Link == "" {
		return nil, fmt.Errorf("razorpay: no UPI intent link in response")
	}
	return &out, nil
}

// ChargeRecurring debits a registered mandate: it creates a recurring order then
// charges the stored token. Returns the payment id. The actual credit grant
// happens on the payment.captured webhook (idempotent), so this only needs to
// initiate the debit. notes tag the student + plan for the webhook.
func (c *Client) ChargeRecurring(amountPaise int, customerID, tokenID, receipt string, notes map[string]string) (string, error) {
	// 1) Recurring order (payment_capture so the debit is auto-captured).
	orderBody := map[string]any{
		"amount":          amountPaise,
		"currency":        "INR",
		"customer_id":     customerID,
		"payment_capture": true,
		"receipt":         receipt,
	}
	if len(notes) > 0 {
		orderBody["notes"] = notes
	}
	var order struct {
		ID string `json:"id"`
	}
	if err := c.post(ordersURL, orderBody, &order); err != nil {
		return "", fmt.Errorf("recurring order: %w", err)
	}
	// 2) Charge the mandate token.
	payBody := map[string]any{
		"amount":      amountPaise,
		"currency":    "INR",
		"order_id":    order.ID,
		"customer_id": customerID,
		"token":       tokenID,
		"recurring":   "1",
	}
	if len(notes) > 0 {
		payBody["notes"] = notes
	}
	var pay struct {
		ID                string `json:"razorpay_payment_id"`
		RazorpayPaymentID string `json:"id"`
	}
	if err := c.post(createRecurringURL, payBody, &pay); err != nil {
		return "", fmt.Errorf("recurring charge: %w", err)
	}
	if pay.ID != "" {
		return pay.ID, nil
	}
	return pay.RazorpayPaymentID, nil
}

// Refund refunds a captured payment. amountPaise <= 0 refunds the full amount.
// Used to return the ₹1 mandate-verification debit after the mandate is set up.
func (c *Client) Refund(paymentID string, amountPaise int) error {
	if paymentID == "" {
		return fmt.Errorf("razorpay: empty payment id for refund")
	}
	body := map[string]any{}
	if amountPaise > 0 {
		body["amount"] = amountPaise
	}
	url := fmt.Sprintf("https://api.razorpay.com/v1/payments/%s/refund", paymentID)
	return c.post(url, body, nil)
}

// VerifyWebhook checks the X-Razorpay-Signature (HMAC-SHA256 of the raw body
// with the webhook secret) in constant time. Returns false if the secret is
// unset so an unconfigured webhook can't be spoofed as valid.
func VerifyWebhook(body []byte, signature, secret string) bool {
	if secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
