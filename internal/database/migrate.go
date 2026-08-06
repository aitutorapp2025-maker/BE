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
		&model.ClassGroup{},
		&model.Book{},
		&model.Plan{},
		&model.Setting{},
		&model.LandingNavItem{},
		&model.LandingStat{},
		&model.LandingFeature{},
		&model.LandingTestimonial{},
		&model.LandingFaq{},
		&model.LandingText{},
		&model.LandingSeo{},
		&model.ContactMessage{},
		&model.DeviceToken{},
		&model.LegalDocument{},
		&model.TeachingLanguage{},
		&model.CreditLedger{},
		&model.PaymentEvent{},
		&model.CronJob{},
		&model.Homework{},
		&model.HomeworkTask{},
		&model.HomeworkTest{},
		&model.AuditLog{},
		&model.Referral{},
		&model.Notification{},
		&model.ChatMessage{},
		&model.AnalyticsDaily{},
		&model.CrashDaily{},
	)
}

// MigrateVectors enables the pgvector extension and migrates the book_chunks
// table (which has a `vector` column). It's separate from Migrate because it
// depends on the pgvector extension being installed on the PostgreSQL server —
// if it isn't, the AI/tutoring features stay off but the rest of the app boots
// normally. Returns false (not an error) when pgvector is unavailable.
func MigrateVectors(db *gorm.DB) (bool, error) {
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		// Extension not installed on the server — skip vector features gracefully.
		return false, nil
	}
	if err := db.AutoMigrate(&model.BookChunk{}); err != nil {
		return false, err
	}
	// ANN index for fast RAG similarity search — without it, every question scans
	// all chunks. HNSW (pgvector ≥ 0.5) with cosine ops matches the `<=>` query.
	// m=16 / ef_construction=64 are pgvector's balanced defaults, set explicitly
	// so the graph quality is predictable as the corpus grows (higher = better
	// recall, slower build). Best-effort: on older pgvector the CREATE fails and
	// we fall back to a scan.
	_ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_book_chunks_embedding ` +
		`ON book_chunks USING hnsw (embedding vector_cosine_ops) ` +
		`WITH (m = 16, ef_construction = 64)`).Error
	// The RAG query filters by class+medium before ordering by vector distance;
	// a btree on the filter columns gives the planner a fast filtered path (HNSW
	// alone doesn't filter well). Best-effort.
	_ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_book_chunks_class_medium ` +
		`ON book_chunks (class_name, medium)`).Error
	// Refresh planner statistics so it costs the new indexes correctly (cheap on
	// an empty/small table at boot; a no-op-ish safety net). Ingest also ANALYZEs
	// after a bulk load so estimates stay accurate as the corpus grows.
	_ = db.Exec(`ANALYZE book_chunks`).Error
	return true, nil
}

// CreateIndexes adds performance indexes that GORM struct tags can't express
// (composite indexes on the hottest queries). Idempotent + best-effort: each
// statement runs independently, so one failure never blocks the others or boot.
// Returns the first error (for a warning log), if any.
func CreateIndexes(db *gorm.DB) error {
	stmts := []string{
		// OTP login looks a student up by phone.
		`CREATE INDEX IF NOT EXISTS idx_students_phone ON students (phone)`,
		// Admin audit viewer: filter by actor_type, newest first.
		`CREATE INDEX IF NOT EXISTS idx_audit_actor_created ON audit_logs (actor_type, created_at DESC)`,
		// Homework history list + delta sync, per student.
		`CREATE INDEX IF NOT EXISTS idx_homeworks_student_created ON homeworks (student_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_homeworks_student_updated ON homeworks (student_id, updated_at)`,
		// Tasks ordered within a homework (preload order_no).
		`CREATE INDEX IF NOT EXISTS idx_hw_tasks_hw_order ON homework_tasks (homework_id, order_no)`,
		// Tests for a homework scoped to a student.
		`CREATE INDEX IF NOT EXISTS idx_hw_tests_hw_student ON homework_tests (homework_id, student_id)`,
		// Recent credit ledger entries per student.
		`CREATE INDEX IF NOT EXISTS idx_credit_student_created ON credit_ledger (student_id, created_at DESC)`,
		// Referrals a student has made, newest first (admin list + student count).
		`CREATE INDEX IF NOT EXISTS idx_referrals_referrer_created ON referrals (referrer_id, created_at DESC)`,
		// Notification feed per student (broadcast rows share student_id 0).
		`CREATE INDEX IF NOT EXISTS idx_notifications_student_created ON notifications (student_id, created_at DESC)`,
		// Referral code uniqueness must be PARTIAL: the code is generated lazily so
		// most rows share '' (empty), and soft-deleted rows are retained — a plain
		// unique index rejects the 2nd empty / a resurrected email (SQLSTATE 23505).
		// Drop any old plain unique index (from the earlier struct tag) and enforce
		// uniqueness only over real, non-deleted codes.
		`DROP INDEX IF EXISTS idx_students_referral_code`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_students_referral_code ON students (referral_code) WHERE referral_code <> '' AND deleted_at IS NULL`,
	}
	var firstErr error
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SeedTeachingLanguages inserts the default teaching languages if none exist.
func SeedTeachingLanguages(db *gorm.DB) (int, error) {
	var count int64
	if err := db.Model(&model.TeachingLanguage{}).Count(&count).Error; err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	langs := []model.TeachingLanguage{
		{Name: "Tamil", Active: true},
		{Name: "English", Active: true},
		{Name: "Hindi", Active: true},
		{Name: "Telugu", Active: true},
	}
	if err := db.Create(&langs).Error; err != nil {
		return 0, err
	}
	return len(langs), nil
}

// termsSeedContent is the default Terms & Conditions body. Lines starting with
// "# " are rendered as section headings in the app.
const termsSeedContent = `Last updated: July 2026

# 1. Acceptance of Terms
By creating an account and using Vaha AI ("the app"), you agree to these Terms & Conditions and our Privacy Policy. If you do not agree, please do not use the app.

# 2. Who can use Vaha AI
Vaha AI is an AI-powered learning tool for students in classes 1-12. If you are under 18, a parent or guardian must review and accept these terms on your behalf and supervise your use.

# 3. Your account
You register with your mobile number and a one-time password (OTP). You are responsible for keeping your account secure and for the information you provide (name, class, board, medium).

# 4. Learning content & AI answers
Explanations, tests and feedback are generated by AI from your textbooks and may occasionally be incomplete or incorrect. Always double-check important answers with your teacher or textbook.

# 5. Uploads
You may upload homework as photos, PDFs or voice notes. Only upload content you have the right to use. We process uploads to create your study plan and do not sell your content.

# 6. Subscriptions & payments
Vaha AI offers a free trial and paid monthly/yearly plans billed securely through Razorpay. Charges, renewals and taxes are shown before you pay.

# 7. Cancellation & refunds
You can cancel anytime from Subscription & billing. Refunds, where applicable, follow our Refund Policy. The free trial can be cancelled before it converts to a paid plan.

# 8. Privacy & data
We collect only what we need to run the service (account, progress, uploads) and protect it as described in our Privacy Policy. Parent contact details are used to send progress updates.

# 9. Acceptable use
Do not misuse the app, attempt to disrupt it, or use it for anything unlawful. We may suspend accounts that violate these terms.

# 10. Changes & contact
We may update these terms and will notify you of important changes. Questions? Contact us at support@vahaai.com.`

// SeedLegal inserts the default Terms & Conditions document if it doesn't exist.
func SeedLegal(db *gorm.DB) (bool, error) {
	var count int64
	if err := db.Model(&model.LegalDocument{}).Where("key = ?", "terms").Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	doc := model.LegalDocument{
		Key:     "terms",
		Title:   "Terms & Conditions",
		Content: termsSeedContent,
	}
	if err := db.Create(&doc).Error; err != nil {
		return false, err
	}
	return true, nil
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

// SeedClassGroups inserts the higher-secondary subject groups if none exist.
// State Board Class 11 & 12 offer Computer Science / Biology / Commerce / Arts
// / Vocational; the admin can add other boards' streams from the class page.
func SeedClassGroups(db *gorm.DB) (int, error) {
	var count int64
	if err := db.Model(&model.ClassGroup{}).Count(&count).Error; err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	names := []string{"Computer Science", "Biology", "Commerce", "Arts", "Vocational"}
	groups := make([]model.ClassGroup, 0, len(names)*2)
	for _, class := range []string{"Class 11", "Class 12"} {
		for i, name := range names {
			groups = append(groups, model.ClassGroup{
				ClassName: class,
				Board:     "State Board",
				Name:      name,
				SortOrder: i,
				Active:    true,
			})
		}
	}
	if err := db.Create(&groups).Error; err != nil {
		return 0, err
	}
	return len(groups), nil
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
	plans := append([]model.Plan{
		{Name: "Free Trial", PriceRupees: 0, DurationDays: 7, Credits: 20, IsTrial: true,
			Tagline: "7 days full access", BestValue: false,
			Features: []string{"All subjects & features", "No card required",
				"Converts to paywall after 7 days"}},
	}, starterPaidPlans()...)
	if err := db.Create(&plans).Error; err != nil {
		return 0, err
	}
	return len(plans), nil
}

// starterPaidPlans are the three paid tiers (₹799 / ₹999 / ₹1299). Credits are
// sized at ~15% of price (1 credit = ₹1 of AI budget) so every plan holds the
// ≥85% gross-margin floor. The ₹999 tier is marked recommended (BestValue).
func starterPaidPlans() []model.Plan {
	return []model.Plan{
		{Name: "Standard", PriceRupees: 799, DurationDays: 30, Credits: 120,
			Tagline: "per month", BestValue: false,
			Features: []string{"Daily AI timetable", "Homework teaching & doubt chat",
				"Written exams + auto-grading", "Parent WhatsApp reports"}},
		{Name: "Pro", PriceRupees: 999, DurationDays: 30, Credits: 150,
			Tagline: "per month", BestValue: true,
			Features: []string{"Everything in Standard", "More AI credits",
				"Oral + written exams", "Priority answers"}},
		{Name: "Premium", PriceRupees: 1299, DurationDays: 30, Credits: 195,
			Tagline: "per month", BestValue: false,
			Features: []string{"Everything in Pro", "Highest AI credits",
				"Best for exam prep", "Priority support"}},
	}
}

// EnsureTrialPlan makes sure exactly one plan is flagged as the trial. On an
// older DB (seeded before is_trial existed) it flags the "Free Trial" plan.
// Non-destructive: does nothing if a trial plan already exists.
func EnsureTrialPlan(db *gorm.DB) error {
	var count int64
	if err := db.Model(&model.Plan{}).Where("is_trial = ?", true).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	// Flag the free (price 0) plan named like a trial, else the cheapest free one.
	return db.Model(&model.Plan{}).
		Where("price_rupees = 0").
		Where("LOWER(name) LIKE ?", "%trial%").
		Update("is_trial", true).Error
}

// EnsureStarterPlans inserts the three paid tiers by name if they're missing.
// It never updates or deletes an existing plan, so admin edits are preserved —
// it only backfills a database that predates these plans.
func EnsureStarterPlans(db *gorm.DB) (int, error) {
	added := 0
	for _, p := range starterPaidPlans() {
		var count int64
		if err := db.Model(&model.Plan{}).Where("name = ?", p.Name).Count(&count).Error; err != nil {
			return added, err
		}
		if count > 0 {
			continue
		}
		if err := db.Create(&p).Error; err != nil {
			return added, err
		}
		added++
	}
	return added, nil
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
