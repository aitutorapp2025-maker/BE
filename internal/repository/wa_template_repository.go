package repository

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WaTemplateRepository stores the local mirror of WhatsApp message templates,
// kept in sync with Meta so the admin list is fast and template ids are cached.
type WaTemplateRepository struct {
	db *gorm.DB
}

// NewWaTemplateRepository builds a WaTemplateRepository.
func NewWaTemplateRepository(db *gorm.DB) *WaTemplateRepository {
	return &WaTemplateRepository{db: db}
}

// Upsert inserts or updates a template keyed by (name, language). Meta-owned
// fields are refreshed; the local primary key + created_at are preserved.
func (r *WaTemplateRepository) Upsert(t *model.WaTemplate) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "name"}, {Name: "language"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"meta_id", "category", "status", "header_format",
			"body_text", "footer", "body_params", "rejected_reason", "buttons",
			"last_synced_at", "updated_at",
		}),
	}).Create(t).Error
}

// SetImageData stores the base64 header image for a template (name+language) so
// campaigns can auto-reuse it without the admin re-uploading each time.
func (r *WaTemplateRepository) SetImageData(name, language, data string) error {
	return r.db.Model(&model.WaTemplate{}).
		Where("name = ? AND language = ?", name, language).
		Update("image_data", data).Error
}

// GetByNameLang returns a template by name + language (for campaign image reuse).
func (r *WaTemplateRepository) GetByNameLang(name, language string) (*model.WaTemplate, error) {
	var t model.WaTemplate
	if err := r.db.Where("name = ? AND language = ?", name, language).
		First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// List returns all templates, newest first.
func (r *WaTemplateRepository) List() ([]model.WaTemplate, error) {
	var out []model.WaTemplate
	err := r.db.Order("updated_at DESC").Find(&out).Error
	return out, err
}

// Get returns one template by local id.
func (r *WaTemplateRepository) Get(id uint) (*model.WaTemplate, error) {
	var t model.WaTemplate
	if err := r.db.First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

// Delete removes a template by local id.
func (r *WaTemplateRepository) Delete(id uint) error {
	return r.db.Delete(&model.WaTemplate{}, id).Error
}

// DeleteByName removes every local row for a template name (all languages), to
// mirror Meta's delete-by-name behaviour.
func (r *WaTemplateRepository) DeleteByName(name string) error {
	return r.db.Where("name = ?", name).Delete(&model.WaTemplate{}).Error
}
