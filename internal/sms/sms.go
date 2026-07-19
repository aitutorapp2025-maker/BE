// Package sms sends text messages via a configurable provider (Nexmo/Vonage or
// SMS Expert). Config is read dynamically so it can come from the DB (admin
// settings) and change at runtime. When not configured, Send is a no-op.
package sms

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Providers.
const (
	ProviderNexmo     = "nexmo"
	ProviderSmsExpert = "smsexpert"
)

// NexmoConfig holds Nexmo/Vonage SMS API credentials.
type NexmoConfig struct {
	APIKey    string
	APISecret string
	From      string
}

// SmsExpertConfig holds SMS Expert (itagg) HTTP-gateway settings. The request
// is: {APIURL}?usr=&pwd=&from=&to=&type=&route=&txt=
type SmsExpertConfig struct {
	APIURL   string // base url, e.g. https://secure.itagg.com/smsg/sms.mes
	User     string // usr
	Password string // pwd
	From     string // from (sender)
	Route    string // route (default "d")
	Type     string // type (default "text")
}

const smsExpertDefaultURL = "https://secure.itagg.com/smsg/sms.mes"

// Config is the resolved SMS configuration.
type Config struct {
	Provider    string
	CountryCode string // e.g. "91" — prepended to bare 10-digit local numbers
	Nexmo       NexmoConfig
	SmsExpert   SmsExpertConfig
}

// Usable reports whether the selected provider has the credentials it needs.
func (c Config) Usable() bool {
	switch c.Provider {
	case ProviderSmsExpert:
		return c.SmsExpert.User != "" && c.SmsExpert.Password != ""
	default:
		return c.Nexmo.APIKey != "" && c.Nexmo.APISecret != ""
	}
}

// ConfigFunc returns the current SMS configuration.
type ConfigFunc func() Config

// Sender sends SMS using the configuration returned by get.
type Sender struct {
	get  ConfigFunc
	http *http.Client
}

// New builds a Sender.
func New(get ConfigFunc) *Sender {
	return &Sender{get: get, http: &http.Client{Timeout: 15 * time.Second}}
}

// Enabled reports whether SMS is currently configured.
func (s *Sender) Enabled() bool { return s.get().Usable() }

// Send delivers a text message. No-op (returns nil) when not configured.
func (s *Sender) Send(to, text string) error {
	c := s.get()
	if !c.Usable() {
		return nil
	}
	to = normalize(to, c.CountryCode)
	switch c.Provider {
	case ProviderSmsExpert:
		return s.sendSmsExpert(c.SmsExpert, to, text)
	default:
		return s.sendNexmo(c.Nexmo, to, text)
	}
}

func (s *Sender) sendNexmo(c NexmoConfig, to, text string) error {
	from := c.From
	if from == "" {
		from = "VahaAI"
	}
	form := url.Values{}
	form.Set("api_key", c.APIKey)
	form.Set("api_secret", c.APISecret)
	form.Set("to", to)
	form.Set("from", from)
	form.Set("text", text)

	resp, err := s.http.PostForm("https://rest.nexmo.com/sms/json", form)
	if err != nil {
		return fmt.Errorf("nexmo request: %w", err)
	}
	defer resp.Body.Close()

	var out struct {
		Messages []struct {
			Status    string `json:"status"`
			ErrorText string `json:"error-text"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("nexmo response: %w", err)
	}
	if len(out.Messages) == 0 {
		return fmt.Errorf("nexmo: empty response")
	}
	if out.Messages[0].Status != "0" {
		return fmt.Errorf("nexmo: %s (status %s)",
			out.Messages[0].ErrorText, out.Messages[0].Status)
	}
	return nil
}

func (s *Sender) sendSmsExpert(c SmsExpertConfig, to, text string) error {
	base := c.APIURL
	if base == "" {
		base = smsExpertDefaultURL
	}
	route := c.Route
	if route == "" {
		route = "d"
	}
	typ := c.Type
	if typ == "" {
		typ = "text"
	}

	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("smsexpert: bad api url: %w", err)
	}
	q := u.Query()
	q.Set("usr", c.User)
	q.Set("pwd", c.Password)
	q.Set("from", c.From)
	q.Set("to", to)
	q.Set("type", typ)
	q.Set("route", route)
	q.Set("txt", text)
	u.RawQuery = q.Encode()

	resp, err := s.http.Get(u.String())
	if err != nil {
		return fmt.Errorf("smsexpert request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("smsexpert: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// normalize cleans a phone number and adds the country code to bare local
// numbers. Rules (with cc e.g. "91"):
//   - strips spaces, dashes, parens and a leading "+"
//   - an 11-digit number with a leading trunk "0" drops the 0
//   - a resulting 10-digit number gets cc prepended
//   - anything already longer (already has a country code) is left unchanged
func normalize(to, cc string) string {
	r := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", "+", "")
	d := r.Replace(strings.TrimSpace(to))
	if cc == "" {
		return d
	}
	if len(d) == 11 && strings.HasPrefix(d, "0") {
		d = d[1:]
	}
	if len(d) == 10 {
		return cc + d
	}
	return d
}
