package middleware

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// AuditEntry is one recorded action, passed to the recorder supplied by routes.
type AuditEntry struct {
	ActorType    string // admin | student
	ActorID      uint
	ActorLabel   string // email (admin) / phone (student)
	Method       string
	Path         string
	Query        string
	Status       int
	IP           string
	UserAgent    string
	LatencyMs    int64
	RequestBody  string // decrypted request payload (sanitized)
	ResponseBody string // plaintext response payload (sanitized)
}

// Audit records each authenticated request via [record]. It runs after the auth
// middleware (so the actor locals are set) AND inside the Encrypt middleware, so
// by the time it reads them the request body is already decrypted and the
// response body is still plaintext (Encrypt encrypts it afterwards). It captures
// the resolved status even when the handler returned an error. Unauthenticated
// requests are skipped. The recorder should persist asynchronously so it never
// blocks the response.
//
// auditSkip lists high-frequency background paths not worth recording (they'd
// flood the trail with polling noise without reflecting a user "action").
var auditSkip = map[string]bool{
	"/api/v1/student/sync": true,
}

func Audit(record func(AuditEntry)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		if auditSkip[c.Path()] {
			return err
		}

		// NOTE: c.Method()/c.Path()/c.IP()/c.Get() return zero-copy strings that
		// alias Fiber's pooled request buffer — reused for the next request. The
		// recorder persists ASYNChronously, so these MUST be cloned or the stored
		// path/method get corrupted (truncated/crossed) once the ctx is recycled.
		e := AuditEntry{
			Method:    strings.Clone(c.Method()),
			Path:      strings.Clone(c.Path()),
			Query:     trunc(string(c.Request().URI().QueryString()), 400),
			IP:        strings.Clone(c.IP()),
			UserAgent: strings.Clone(trunc(c.Get(fiber.HeaderUserAgent), 300)),
			LatencyMs: time.Since(start).Milliseconds(),
			Status:    c.Response().StatusCode(),
		}
		if err != nil {
			if fe, ok := err.(*fiber.Error); ok {
				e.Status = fe.Code
			} else {
				e.Status = fiber.StatusInternalServerError
			}
		}

		// Capture the (decrypted) request and (plaintext) response payloads —
		// sanitized: secrets redacted, size-capped, binary/multipart omitted.
		// WRITE operations only: GET/HEAD responses (list/detail reads) are the
		// bulk of traffic and their payloads add nothing to a "who changed what"
		// trail, so they aren't stored — at lakh-student scale they would
		// otherwise dominate the database. Method/path/query/status are still
		// recorded for every request.
		if e.Method != fiber.MethodGet && e.Method != fiber.MethodHead {
			e.RequestBody = sanitizeBody(c.Body(), string(c.Request().Header.ContentType()))
			e.ResponseBody = sanitizeBody(c.Response().Body(), string(c.Response().Header.ContentType()))
		}

		if id, ok := c.Locals("admin_id").(uint); ok && id != 0 {
			e.ActorType = "admin"
			e.ActorID = id
			if s, ok := c.Locals("email").(string); ok {
				e.ActorLabel = s
			}
		} else if id, ok := c.Locals("student_id").(uint); ok && id != 0 {
			e.ActorType = "student"
			e.ActorID = id
			if s, ok := c.Locals("phone").(string); ok {
				e.ActorLabel = s
			}
		} else {
			return err // not authenticated — nothing to attribute
		}

		record(e)
		return err
	}
}

// maxAuditBody caps a stored payload; larger ones are truncated with a marker.
// 2KB keeps the review trail useful (a full form submit fits) while bounding
// per-row storage — at 100k students the audit table is the largest in the DB.
const maxAuditBody = 2 * 1024

// sanitizeBody returns a safe, size-capped, secret-redacted string for a request
// or response payload. Non-JSON bodies (multipart uploads, binary) are not
// logged — only their type + size is noted.
func sanitizeBody(raw []byte, contentType string) string {
	if len(raw) == 0 {
		return ""
	}
	if !strings.Contains(strings.ToLower(contentType), "json") {
		ct := contentType
		if i := strings.IndexByte(ct, ';'); i >= 0 {
			ct = ct[:i]
		}
		ct = strings.TrimSpace(ct)
		if ct == "" {
			ct = "binary"
		}
		return fmt.Sprintf("[%s body, %d bytes — not logged]", ct, len(raw))
	}
	out := redactJSON(raw)
	if len(out) > maxAuditBody {
		return string(out[:maxAuditBody]) + "…(truncated)"
	}
	return string(out)
}

// redactJSON replaces the values of secret-looking keys with "[redacted]" while
// preserving structure. Returns the input unchanged if it isn't valid JSON.
func redactJSON(raw []byte) []byte {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	redactValue(v)
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

func redactValue(v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isSensitiveKey(k) {
				t[k] = "[redacted]"
				continue
			}
			redactValue(val)
		}
	case []any:
		for _, val := range t {
			redactValue(val)
		}
	}
}

// isSensitiveKey reports whether a JSON field name looks like a credential.
func isSensitiveKey(k string) bool {
	k = strings.ToLower(k)
	for _, s := range []string{
		"password", "secret", "token", "api_key", "apikey",
		"client_pub", "signing_secret", "authorization",
	} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// trunc caps a string to n bytes.
func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
