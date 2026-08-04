package middleware

import "github.com/gofiber/fiber/v2"

// AuditEntry is one recorded action, passed to the recorder supplied by routes.
type AuditEntry struct {
	ActorType  string // admin | student
	ActorID    uint
	ActorLabel string // email (admin) / phone (student)
	Method     string
	Path       string
	Status     int
	IP         string
}

// Audit records each authenticated request via [record]. It runs after the auth
// middleware (so the actor locals are set) and captures the resolved status even
// when the handler returned an error. Unauthenticated requests are skipped. The
// recorder should persist asynchronously so it never blocks the response.
// auditSkip lists high-frequency background paths not worth recording (they'd
// flood the trail with polling noise without reflecting a user "action").
var auditSkip = map[string]bool{
	"/api/v1/student/sync": true,
}

func Audit(record func(AuditEntry)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		err := c.Next()

		if auditSkip[c.Path()] {
			return err
		}

		e := AuditEntry{
			Method: c.Method(),
			Path:   c.Path(),
			IP:     c.IP(),
			Status: c.Response().StatusCode(),
		}
		if err != nil {
			if fe, ok := err.(*fiber.Error); ok {
				e.Status = fe.Code
			} else {
				e.Status = fiber.StatusInternalServerError
			}
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
