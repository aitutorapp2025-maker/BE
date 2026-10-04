package service

import (
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// PushEnqueue delivers a notification (push + in-app feed). An empty studentIDs
// slice broadcasts to all customers.
type PushEnqueue func(studentIDs []uint, title, body, typ string)

// NotificationService renders an automated notification from its admin-editable
// template (falling back to the built-in default) and enqueues it for delivery.
type NotificationService struct {
	templates *repository.NotificationTemplateRepository
	enqueue   PushEnqueue
}

// NewNotificationService builds a NotificationService.
func NewNotificationService(templates *repository.NotificationTemplateRepository, enqueue PushEnqueue) *NotificationService {
	return &NotificationService{templates: templates, enqueue: enqueue}
}

// Send resolves the template for typ (admin wording if set + active, else the
// provided defaults), substitutes {placeholders} from vars, and enqueues it.
// Returns false when the admin has turned this notification off. studentIDs
// empty = broadcast to all.
func (s *NotificationService) Send(studentIDs []uint, typ string, vars map[string]string, defTitle, defBody string) bool {
	if s == nil || s.enqueue == nil {
		return false
	}
	title, body := defTitle, defBody
	if t, err := s.templates.Get(typ); err == nil && t != nil {
		if !t.Active {
			return false // admin disabled this notification
		}
		if strings.TrimSpace(t.Title) != "" {
			title = t.Title
		}
		if strings.TrimSpace(t.Body) != "" {
			body = t.Body
		}
	}
	for k, v := range vars {
		ph := "{" + k + "}"
		title = strings.ReplaceAll(title, ph, v)
		body = strings.ReplaceAll(body, ph, v)
	}
	s.enqueue(studentIDs, title, body, typ)
	return true
}
