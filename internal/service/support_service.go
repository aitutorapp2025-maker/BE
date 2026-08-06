package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/media"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// maxSupportAttachment caps an uploaded report file.
const maxSupportAttachment = 12 << 20 // 12 MB

// SupportService creates + manages "Report a problem" tickets, storing any
// uploaded attachment privately (served via a signed /media/support URL).
type SupportService struct {
	tickets     *repository.SupportRepository
	privateDir  string
	publicBase  string
	mediaSecret string
}

// NewSupportService builds a SupportService.
func NewSupportService(tickets *repository.SupportRepository, privateDir, publicBase, mediaSecret string) *SupportService {
	return &SupportService{tickets: tickets, privateDir: privateDir, publicBase: publicBase, mediaSecret: mediaSecret}
}

// Create files a new ticket, optionally saving an attachment (image/PDF bytes).
func (s *SupportService) Create(studentID uint, label, message string, attachment []byte, mediaType string) (*model.SupportTicket, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, fmt.Errorf("please describe the problem")
	}
	url := ""
	if len(attachment) > 0 {
		if len(attachment) > maxSupportAttachment {
			return nil, fmt.Errorf("that file is too large — please attach a smaller one")
		}
		u, err := s.saveAttachment(attachment, mediaType)
		if err != nil {
			return nil, fmt.Errorf("save attachment: %w", err)
		}
		url = u
	}
	t := &model.SupportTicket{
		StudentID:     studentID,
		StudentLabel:  label,
		Message:       message,
		AttachmentURL: url,
		Status:        "open",
	}
	if err := s.tickets.Create(t); err != nil {
		return nil, err
	}
	return t, nil
}

// Reply sets the admin response + status on a ticket.
func (s *SupportService) Reply(id uint, reply, status string) (*model.SupportTicket, error) {
	t, err := s.tickets.Get(id)
	if err != nil {
		return nil, err
	}
	switch status {
	case "open", "in_progress", "resolved":
		t.Status = status
	}
	reply = strings.TrimSpace(reply)
	if reply != "" && reply != t.AdminReply {
		t.AdminReply = reply
		now := time.Now()
		t.RepliedAt = &now
	}
	if err := s.tickets.Update(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *SupportService) saveAttachment(data []byte, mediaType string) (string, error) {
	dir := filepath.Join(s.privateDir, "support")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := ".jpg"
	switch {
	case strings.Contains(mediaType, "png"):
		ext = ".png"
	case strings.Contains(mediaType, "pdf"):
		ext = ".pdf"
	}
	rnd := make([]byte, 16)
	if _, err := rand.Read(rnd); err != nil {
		return "", err
	}
	name := hex.EncodeToString(rnd) + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	base := strings.TrimRight(s.publicBase, "/")
	return base + "/api/v1/media/support/" + name + "?sig=" + media.Sign(name, s.mediaSecret), nil
}
