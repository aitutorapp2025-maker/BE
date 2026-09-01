package worker

import (
	"encoding/json"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/queue"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/wa"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// StartWaInboxWorker consumes raw Meta webhook payloads from RabbitMQ and
// stores the customer messages for the admin WhatsApp chat. Jobs are always
// acked — a malformed payload is logged and dropped, never retried forever.
func StartWaInboxWorker(mq *queue.RabbitMQ, messages *repository.WaMessageRepository, log *logger.Logger) error {
	return mq.Consume(wa.QueueWaInbox, func(body []byte) error {
		var payload struct {
			Entry []struct {
				Changes []struct {
					Value struct {
						Contacts []struct {
							WaID    string `json:"wa_id"`
							Profile struct {
								Name string `json:"name"`
							} `json:"profile"`
						} `json:"contacts"`
						Messages []struct {
							From string `json:"from"`
							ID   string `json:"id"`
							Type string `json:"type"`
							Text struct {
								Body string `json:"body"`
							} `json:"text"`
							Button struct {
								Text string `json:"text"`
							} `json:"button"`
						} `json:"messages"`
					} `json:"value"`
				} `json:"changes"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Errorf("wa inbox: bad payload: %v", err)
			return nil
		}
		saved := 0
		for _, e := range payload.Entry {
			for _, ch := range e.Changes {
				names := map[string]string{}
				for _, ct := range ch.Value.Contacts {
					names[ct.WaID] = ct.Profile.Name
				}
				for _, m := range ch.Value.Messages {
					text := strings.TrimSpace(m.Text.Body)
					if text == "" && m.Button.Text != "" {
						text = m.Button.Text // template quick-reply taps
					}
					if text == "" && m.Type != "text" {
						text = "[" + m.Type + "]" // image/audio/document/sticker
					}
					if err := messages.Save(&model.WaMessage{
						WaMsgID:   m.ID,
						Phone:     m.From,
						Name:      names[m.From],
						Direction: "in",
						MsgType:   m.Type,
						Text:      text,
					}); err != nil {
						log.Errorf("wa inbox: save: %v", err)
						continue
					}
					saved++
				}
			}
		}
		if saved > 0 {
			log.Infof("wa inbox: stored %d incoming message(s)", saved)
		}
		return nil
	})
}
