package repository

import (
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// WaCampaignRepository persists broadcast campaigns and their recipients, and
// tracks per-recipient delivery status as the worker sends.
type WaCampaignRepository struct {
	db *gorm.DB
}

// NewWaCampaignRepository builds a WaCampaignRepository.
func NewWaCampaignRepository(db *gorm.DB) *WaCampaignRepository {
	return &WaCampaignRepository{db: db}
}

// Create inserts a campaign (ID is populated on the struct).
func (r *WaCampaignRepository) Create(c *model.WaCampaign) error {
	return r.db.Create(c).Error
}

// AddRecipients bulk-inserts recipients (IDs are populated on each struct).
func (r *WaCampaignRepository) AddRecipients(recs []*model.WaCampaignRecipient) error {
	if len(recs) == 0 {
		return nil
	}
	return r.db.CreateInBatches(recs, 500).Error
}

// List returns campaigns, newest first.
func (r *WaCampaignRepository) List(limit int) ([]model.WaCampaign, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var out []model.WaCampaign
	err := r.db.Order("created_at DESC").Limit(limit).Find(&out).Error
	return out, err
}

// Get returns one campaign by id.
func (r *WaCampaignRepository) Get(id uint) (*model.WaCampaign, error) {
	var c model.WaCampaign
	if err := r.db.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// Recipients returns a campaign's recipients (for the report).
func (r *WaCampaignRepository) Recipients(campaignID uint, limit int) ([]model.WaCampaignRecipient, error) {
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	var out []model.WaCampaignRecipient
	err := r.db.Where("campaign_id = ?", campaignID).
		Order("id ASC").Limit(limit).Find(&out).Error
	return out, err
}

// MarkSent flags a recipient delivered and bumps the campaign's sent counter.
func (r *WaCampaignRepository) MarkSent(recipientID, campaignID uint, wamid string) error {
	now := time.Now()
	if err := r.db.Model(&model.WaCampaignRecipient{}).Where("id = ?", recipientID).
		Updates(map[string]any{
			"status": "sent", "wa_msg_id": wamid, "error": "", "sent_at": now,
		}).Error; err != nil {
		return err
	}
	return r.bump(campaignID, "sent")
}

// MarkFailed flags a recipient failed and bumps the campaign's failed counter.
func (r *WaCampaignRepository) MarkFailed(recipientID, campaignID uint, msg string) error {
	if len(msg) > 400 {
		msg = msg[:400]
	}
	if err := r.db.Model(&model.WaCampaignRecipient{}).Where("id = ?", recipientID).
		Updates(map[string]any{"status": "failed", "error": msg}).Error; err != nil {
		return err
	}
	return r.bump(campaignID, "failed")
}

// bump increments a campaign counter and finalizes status to "done" once every
// recipient has been processed (sent + failed >= total).
func (r *WaCampaignRepository) bump(campaignID uint, field string) error {
	if err := r.db.Model(&model.WaCampaign{}).Where("id = ?", campaignID).
		UpdateColumn(field, gorm.Expr(field+" + 1")).Error; err != nil {
		return err
	}
	return r.db.Exec(
		`UPDATE wa_campaigns SET status='done'
		 WHERE id = ? AND status='sending' AND (sent + failed) >= total`,
		campaignID).Error
}

// DueScheduled returns campaigns whose scheduled time has arrived.
func (r *WaCampaignRepository) DueScheduled(now time.Time) ([]model.WaCampaign, error) {
	var out []model.WaCampaign
	err := r.db.Where("status = ? AND scheduled_at IS NOT NULL AND scheduled_at <= ?",
		"scheduled", now).Find(&out).Error
	return out, err
}

// StartSending marks a scheduled campaign as sending and sets the header media
// id uploaded at dispatch time.
func (r *WaCampaignRepository) StartSending(id uint, mediaID string) error {
	return r.db.Model(&model.WaCampaign{}).Where("id = ?", id).
		Updates(map[string]any{"status": "sending", "header_image_id": mediaID}).Error
}

// SetStatus updates a campaign's status.
func (r *WaCampaignRepository) SetStatus(campaignID uint, status string) error {
	return r.db.Model(&model.WaCampaign{}).Where("id = ?", campaignID).
		Update("status", status).Error
}

// AllRecipients returns every recipient of a campaign (for resend).
func (r *WaCampaignRepository) AllRecipients(campaignID uint) ([]model.WaCampaignRecipient, error) {
	var out []model.WaCampaignRecipient
	err := r.db.Where("campaign_id = ?", campaignID).Find(&out).Error
	return out, err
}

// Rename updates a campaign's display name.
func (r *WaCampaignRepository) Rename(id uint, name string) error {
	return r.db.Model(&model.WaCampaign{}).Where("id = ?", id).
		Update("name", name).Error
}

// Delete removes a campaign and all its recipient rows.
func (r *WaCampaignRepository) Delete(id uint) error {
	if err := r.db.Where("campaign_id = ?", id).
		Delete(&model.WaCampaignRecipient{}).Error; err != nil {
		return err
	}
	return r.db.Delete(&model.WaCampaign{}, id).Error
}

// ResetForResend clears every recipient back to queued and zeroes the campaign
// counters so the whole list can be sent again.
func (r *WaCampaignRepository) ResetForResend(id uint) error {
	if err := r.db.Model(&model.WaCampaignRecipient{}).Where("campaign_id = ?", id).
		Updates(map[string]any{
			"status": "queued", "error": "", "wa_msg_id": "", "sent_at": nil,
		}).Error; err != nil {
		return err
	}
	return r.db.Model(&model.WaCampaign{}).Where("id = ?", id).
		Updates(map[string]any{"sent": 0, "failed": 0, "status": "sending"}).Error
}

// FailedRecipients returns the failed recipients of a campaign (for retry).
func (r *WaCampaignRepository) FailedRecipients(campaignID uint) ([]model.WaCampaignRecipient, error) {
	var out []model.WaCampaignRecipient
	err := r.db.Where("campaign_id = ? AND status = ?", campaignID, "failed").
		Find(&out).Error
	return out, err
}

// RequeueFailed resets the given recipients to queued and adjusts the campaign
// counters/status so a retry can re-send them.
func (r *WaCampaignRepository) RequeueFailed(campaignID uint, recipientIDs []uint) error {
	if len(recipientIDs) == 0 {
		return nil
	}
	if err := r.db.Model(&model.WaCampaignRecipient{}).
		Where("id IN ?", recipientIDs).
		Updates(map[string]any{"status": "queued", "error": ""}).Error; err != nil {
		return err
	}
	return r.db.Model(&model.WaCampaign{}).Where("id = ?", campaignID).
		Updates(map[string]any{
			"failed": gorm.Expr("GREATEST(failed - ?, 0)", len(recipientIDs)),
			"status": "sending",
		}).Error
}
