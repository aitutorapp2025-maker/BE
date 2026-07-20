package repository

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeviceTokenRepository provides data access for FCM device tokens.
type DeviceTokenRepository struct {
	db *gorm.DB
}

// NewDeviceTokenRepository builds a DeviceTokenRepository.
func NewDeviceTokenRepository(db *gorm.DB) *DeviceTokenRepository {
	return &DeviceTokenRepository{db: db}
}

// Register upserts a device token by its value (called at app open, before
// login). If the token already exists its platform is refreshed; any existing
// student mapping is preserved.
func (r *DeviceTokenRepository) Register(token, platform string) error {
	dt := model.DeviceToken{Token: token, Platform: platform}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "token"}},
		DoUpdates: clause.AssignmentColumns([]string{"platform", "updated_at"}),
	}).Create(&dt).Error
}

// Map associates a token with a student + mobile number, inserting the row if
// the token is new (e.g. mapped straight from OTP verification without a prior
// register call). Platform is only set on insert; it isn't clobbered on update.
func (r *DeviceTokenRepository) Map(token, platform string, studentID uint, phone string) error {
	dt := model.DeviceToken{
		Token:     token,
		Platform:  platform,
		StudentID: &studentID,
		Phone:     phone,
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "token"}},
		DoUpdates: clause.AssignmentColumns([]string{"student_id", "phone", "updated_at"}),
	}).Create(&dt).Error
}

// TokensForStudent returns all device tokens mapped to a student (for sending
// push notifications to every device the student is signed in on).
func (r *DeviceTokenRepository) TokensForStudent(studentID uint) ([]string, error) {
	var tokens []string
	err := r.db.Model(&model.DeviceToken{}).
		Where("student_id = ?", studentID).
		Pluck("token", &tokens).Error
	return tokens, err
}
