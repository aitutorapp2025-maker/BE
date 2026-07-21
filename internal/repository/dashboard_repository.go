package repository

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// DashboardStats are the aggregate counts shown on the admin dashboard,
// computed server-side (no need to download every row to the client).
type DashboardStats struct {
	TotalStudents   int64 `json:"total_students"`
	PaidStudents    int64 `json:"paid_students"`
	TrialStudents   int64 `json:"trial_students"`
	ExpiredStudents int64 `json:"expired_students"`
	Revenue         int64 `json:"revenue"`
	Classes         int64 `json:"classes"`
	Books           int64 `json:"books"`
	Plans           int64 `json:"plans"`
	Enquiries       int64 `json:"enquiries"`
}

// DashboardRepository computes admin dashboard aggregates.
type DashboardRepository struct {
	db *gorm.DB
}

// NewDashboardRepository builds a DashboardRepository.
func NewDashboardRepository(db *gorm.DB) *DashboardRepository {
	return &DashboardRepository{db: db}
}

// Stats runs the aggregate count/sum queries for the dashboard.
func (r *DashboardRepository) Stats() (*DashboardStats, error) {
	var s DashboardStats

	if err := r.db.Model(&model.Student{}).Count(&s.TotalStudents).Error; err != nil {
		return nil, err
	}
	r.db.Model(&model.Student{}).Where("pay_status = ?", "paid").Count(&s.PaidStudents)
	r.db.Model(&model.Student{}).Where("pay_status = ?", "trial").Count(&s.TrialStudents)
	r.db.Model(&model.Student{}).Where("pay_status = ?", "expired").Count(&s.ExpiredStudents)
	r.db.Model(&model.SchoolClass{}).Count(&s.Classes)
	r.db.Model(&model.Book{}).Count(&s.Books)
	r.db.Model(&model.Plan{}).Count(&s.Plans)
	r.db.Model(&model.ContactMessage{}).Count(&s.Enquiries)

	// Revenue = sum of each paid student's plan price. Student.plan ("monthly"/
	// "yearly") matches Plan.name case-insensitively.
	r.db.Raw(`SELECT COALESCE(SUM(p.price_rupees), 0)
		FROM students st JOIN plans p ON LOWER(p.name) = LOWER(st.plan)
		WHERE st.pay_status = 'paid'`).Scan(&s.Revenue)

	return &s, nil
}

// RecentStudents returns the newest students for the dashboard panel.
func (r *DashboardRepository) RecentStudents(limit int) ([]model.Student, error) {
	var students []model.Student
	err := r.db.Order("created_at DESC").Limit(limit).Find(&students).Error
	return students, err
}
