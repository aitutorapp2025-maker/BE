package database

import (
	"fmt"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Seeded demo admin credentials (development only). The FE admin login
// pre-fills this email — change/remove before production.
const (
	seedAdminName     = "Super Admin"
	seedAdminEmail    = "admin@vahaai.com"
	seedAdminPassword = "Admin@123"
)

// Migrate runs GORM auto-migration for all models.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.Admin{},
		&model.Student{},
		&model.SchoolClass{},
		&model.Book{},
		&model.Plan{},
		&model.Setting{},
		&model.LandingNavItem{},
		&model.LandingStat{},
		&model.LandingFeature{},
		&model.LandingTestimonial{},
		&model.LandingFaq{},
		&model.LandingText{},
		&model.ContactMessage{},
	)
}

// SeedClasses inserts Class 1 … Class 12 if the table is empty.
func SeedClasses(db *gorm.DB) (int, error) {
	var count int64
	if err := db.Model(&model.SchoolClass{}).Count(&count).Error; err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	classes := make([]model.SchoolClass, 0, 12)
	for i := 1; i <= 12; i++ {
		classes = append(classes, model.SchoolClass{
			Name: fmt.Sprintf("Class %d", i), Number: i, Active: true,
		})
	}
	if err := db.Create(&classes).Error; err != nil {
		return 0, err
	}
	return len(classes), nil
}

// SeedBooks inserts a few demo books if the table is empty.
func SeedBooks(db *gorm.DB) (int, error) {
	var count int64
	if err := db.Model(&model.Book{}).Count(&count).Error; err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	books := []model.Book{
		{Title: "Science Textbook", ClassName: "Class 10", Subject: "Science",
			Medium: "English", Publisher: "Tamil Nadu Board", Status: "Indexed"},
		{Title: "அறிவியல் பாடநூல்", ClassName: "Class 10", Subject: "Science",
			Medium: "Tamil", Publisher: "Tamil Nadu Board", Status: "Indexed"},
		{Title: "Mathematics", ClassName: "Class 8", Subject: "Maths",
			Medium: "English", Publisher: "NCERT", Status: "Processing"},
		{Title: "Social Science", ClassName: "Class 9", Subject: "Social",
			Medium: "English", Publisher: "Tamil Nadu Board", Status: "Pending"},
	}
	if err := db.Create(&books).Error; err != nil {
		return 0, err
	}
	return len(books), nil
}

// SeedPlans inserts the default subscription plans if the table is empty.
func SeedPlans(db *gorm.DB) (int, error) {
	var count int64
	if err := db.Model(&model.Plan{}).Count(&count).Error; err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	mrp := 3588
	plans := []model.Plan{
		{Name: "Free Trial", PriceRupees: 0, DurationDays: 7,
			Tagline: "7 days full access", BestValue: false,
			Features: []string{"All subjects & features", "No card required",
				"Converts to paywall after 7 days"}},
		{Name: "Monthly", PriceRupees: 299, DurationDays: 30,
			Tagline: "per month", BestValue: false,
			Features: []string{"Unlimited homework uploads",
				"AI explanations & doubt chat", "Oral + written + reading tests",
				"Parent progress reports"}},
		{Name: "Yearly", PriceRupees: 2499, MrpRupees: &mrp, DurationDays: 365,
			Tagline: "per year", BestValue: true,
			Features: []string{"Everything in Monthly",
				"Best value — save vs monthly", "Priority support"}},
	}
	if err := db.Create(&plans).Error; err != nil {
		return 0, err
	}
	return len(plans), nil
}

// SeedSettings inserts the single default settings row if none exists.
func SeedSettings(db *gorm.DB) (bool, error) {
	var count int64
	if err := db.Model(&model.Setting{}).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	s := model.Setting{
		ID: 1, AppName: "Vaha AI", SupportEmail: "support@vahaai.com",
		EmailNotifications: true, AutoApproveAnswers: false, MaintenanceMode: false,
	}
	if err := db.Create(&s).Error; err != nil {
		return false, err
	}
	return true, nil
}

// SeedStudents inserts a few demo students if the table is empty. Returns the
// number of rows inserted.
func SeedStudents(db *gorm.DB) (int, error) {
	var count int64
	if err := db.Model(&model.Student{}).Count(&count).Error; err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}

	now := time.Now()
	ago := func(days int) time.Time { return now.AddDate(0, 0, -days) }

	students := []model.Student{
		{Name: "Aarav Kumar", Phone: "9876543210", ParentPhone: "9876543211",
			StudentClass: "Class 10", Board: "State Board", Medium: "English",
			Plan: "yearly", PayStatus: "paid", JoinedAt: ago(40)},
		{Name: "Divya S", Phone: "9812345670", ParentPhone: "9812345671",
			StudentClass: "Class 8", Board: "State Board", Medium: "Tamil",
			Plan: "monthly", PayStatus: "paid", JoinedAt: ago(20)},
		{Name: "Rahul Nair", Phone: "9900112233", ParentPhone: "9900112234",
			StudentClass: "Class 12", Board: "CBSE", Medium: "English",
			Plan: "trial", PayStatus: "trial", JoinedAt: ago(3)},
		{Name: "Meena R", Phone: "9445566778", ParentPhone: "9445566779",
			StudentClass: "Class 6", Board: "State Board", Medium: "Tamil",
			Plan: "monthly", PayStatus: "expired", JoinedAt: ago(70)},
		{Name: "Karthik V", Phone: "9333344455", ParentPhone: "9333344456",
			StudentClass: "Class 9", Board: "ICSE", Medium: "English",
			Plan: "yearly", PayStatus: "paid", JoinedAt: ago(12)},
	}
	if err := db.Create(&students).Error; err != nil {
		return 0, err
	}
	return len(students), nil
}

// SeedAdmin inserts a demo admin if no admins exist yet. Returns true if seeded.
func SeedAdmin(db *gorm.DB) (bool, error) {
	var count int64
	if err := db.Model(&model.Admin{}).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(seedAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return false, fmt.Errorf("hash seed password: %w", err)
	}

	admin := model.Admin{
		Name:         seedAdminName,
		Email:        seedAdminEmail,
		PasswordHash: string(hash),
		Role:         "admin",
		IsActive:     true,
	}
	if err := db.Create(&admin).Error; err != nil {
		return false, err
	}
	return true, nil
}
