package database

import (
	"fmt"
	"strings"
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
		&model.AdminRole{}, // before Admin (admins.role_id references it)
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
		&model.HomeworkDoubt{},
		&model.AuditLog{},
		&model.Referral{},
		&model.Notification{},
		&model.ChatMessage{},
		&model.ChatConversation{},
		&model.AnalyticsDaily{},
		&model.CrashDaily{},
		&model.SupportTicket{},
		&model.HomeBanner{},
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
		// Recent chat window per student (capped fetch in ListByStudent, sorted by sent_at).
		`CREATE INDEX IF NOT EXISTS idx_chat_student_sent ON chat_messages (student_id, sent_at DESC)`,
		// Drop redundant single-column indexes: each is fully covered by the LEADING
		// column of a composite index above, so the planner never uses it — it only
		// adds write overhead. (The matching gorm:"index" struct tags were removed so
		// AutoMigrate won't recreate them.) StudentID indexes that are NOT a composite
		// leading column (homework_tests, payment_events, support_tickets, device_tokens)
		// and the standalone created_at indexes are intentionally kept.
		`DROP INDEX IF EXISTS idx_homeworks_student_id`,
		`DROP INDEX IF EXISTS idx_homework_tasks_homework_id`,
		`DROP INDEX IF EXISTS idx_homework_tests_homework_id`,
		`DROP INDEX IF EXISTS idx_credit_ledger_student_id`,
		`DROP INDEX IF EXISTS idx_notifications_student_id`,
		`DROP INDEX IF EXISTS idx_referrals_referrer_id`,
		`DROP INDEX IF EXISTS idx_audit_logs_actor_type`,
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

// SeedTeachingLanguages inserts any DEFAULT teaching language that is missing
// (by name, case-insensitive). Idempotent: existing rows — including ones the
// admin renamed or disabled — are never touched, so it doubles as the
// migration that adds newly-introduced languages to older databases.
func SeedTeachingLanguages(db *gorm.DB) (int, error) {
	var existing []model.TeachingLanguage
	if err := db.Find(&existing).Error; err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, l := range existing {
		have[strings.ToLower(strings.TrimSpace(l.Name))] = true
	}
	added := 0
	for _, name := range []string{
		"Tamil", "English", "Hindi", "Telugu", "Kannada", "Malayalam", "Urdu",
	} {
		if have[strings.ToLower(name)] {
			continue
		}
		if err := db.Create(&model.TeachingLanguage{Name: name, Active: true}).Error; err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// termsSeedContent is the default Terms & Conditions body. Lines starting
// with "# " are rendered as section headings in the app.
const termsSeedContent = `Last updated: August 2026

# 1. Acceptance of Terms
By creating an account and using Vaha AI ("the app"), operated by KA Software ("we", "us"), you agree to these Terms & Conditions, our Privacy Policy and our Refund Policy. If you do not agree, please do not use the app.

# 2. Who can use Vaha AI
Vaha AI is an AI-powered learning tool for students in classes 1-12. If you are under 18, a parent or guardian must review and accept these terms on your behalf and supervise your use.

# 3. Your account
You register with your mobile number and a one-time password (OTP). You are responsible for keeping your device, OTPs and account secure and for the information you provide. We are not responsible for access to your account that results from you sharing your OTP or device.

# 4. Vaha AI can make mistakes — AI accuracy disclaimer
Vaha AI uses artificial intelligence. AI-generated explanations, answers, translations, voice narration, study plans, test questions, gradings and feedback CAN BE WRONG, incomplete, outdated or misleading. Vaha AI is a learning aid only — it is not a teacher, not professional advice, and not a guarantee of marks, exam success or admission. Always verify important answers with your school teacher and official textbooks. We accept no responsibility or liability for any loss, poor result or decision caused by relying on AI-generated content.

# 5. Uploads
You may upload homework as photos, PDFs, documents or voice notes. Only upload content you have the right to use. We process uploads to create your study plan and lessons; we do not sell your content. You are responsible for what you upload.

# 6. Subscriptions & payments
Vaha AI offers a free trial and paid plans billed securely through Razorpay. Charges, renewals and taxes are shown before you pay. Prices and plan features may change; changes apply from your next billing cycle.

# 7. Cancellation & refunds
You can cancel anytime from Subscription & billing; access continues until the end of the paid period. All payments are final and non-refundable except as expressly stated in our Refund Policy. No refunds are due for dissatisfaction with AI answers, downtime, feature changes, unused days, data loss or suspension of an account that broke these terms.

# 8. Privacy & data
We collect only what we need to run the service (account, progress, uploads, parent contact for progress reports) and handle it as described in our Privacy Policy.

# 9. Service provided "as is" — no warranties
The app is provided on an "as is" and "as available" basis, without warranties of any kind, express or implied — including accuracy, availability, fitness for a particular purpose or uninterrupted, error-free operation. Features may change, be interrupted or be withdrawn at any time without notice.

# 10. Data loss & security incidents
We use reasonable technical safeguards (encryption in transit, signed requests, access controls), but no system connected to the internet is 100% secure. To the maximum extent permitted by law, we are NOT liable for any loss, corruption, leak, breach, unauthorised access, hacking or misuse of data caused by third parties, technical failure or events beyond our reasonable control, and no compensation or refund is payable for such events. Where the law requires, we will take reasonable remedial steps and inform affected users of a serious incident.

# 11. Limitation of liability
To the maximum extent permitted by law: (a) we are not liable for any indirect, incidental, special or consequential loss — including loss of data, marks, opportunities, goodwill or profits; (b) our total combined liability for all claims relating to the app is limited to the amount you actually paid us in the 30 days before the claim arose (or ₹100 if you paid nothing); (c) nothing in these terms limits liability that cannot be limited under applicable law.

# 12. Your responsibility & indemnity
You agree to use the app lawfully and not to misuse, disrupt, copy, reverse-engineer or resell it. You agree to compensate (indemnify) us against claims, damages and costs arising from your misuse of the app, your uploads or your breach of these terms. We may suspend or terminate accounts that violate these terms without refund.

# 13. Force majeure
We are not responsible for delays or failures caused by events beyond our reasonable control — including network, power or hosting failures, third-party service outages (SMS, WhatsApp, payment gateways, AI providers), natural disasters, strikes or government action.

# 14. Governing law & disputes
These terms are governed by the laws of India. Courts in Tamil Nadu, India have exclusive jurisdiction over any dispute relating to the app.

# 15. Changes & contact
We may update these terms at any time and will notify you of important changes in the app; continued use after changes means acceptance. Questions? Contact us at support@vahaai.com.`

// privacySeedContent is the default Privacy Policy body.
const privacySeedContent = `Last updated: August 2026

# 1. Who we are
Vaha AI is operated by KA Software ("we", "us"). This policy explains what data we collect, why, and how we protect it when you use the Vaha AI app and website.

# 2. Data we collect
Account data: your name, mobile number, medium of study, teaching language and (optional) profile details. Parent contact: the WhatsApp number you give us for progress reports. Learning data: homework you upload (photos, PDFs, documents, voice notes), questions you ask, lessons, doubts, test answers, marks and progress. Technical data: device push token, app version and basic logs needed to run and secure the service.

# 3. How we use your data
To run the service: reading your homework with AI, creating study plans and lessons in your chosen language, grading tests, tracking progress. To communicate: login OTPs by SMS and WhatsApp, study reminders by push notification, and daily study reports to the parent WhatsApp number. To improve safety and prevent abuse. We do NOT sell your personal data.

# 4. AI processing
To teach you, your uploads and questions are processed by third-party AI providers (for example large-language-model and speech services). Only what is needed to generate the lesson or answer is sent, and it is used to provide the service — remember that AI output can contain mistakes (see our Terms & Conditions).

# 5. Third-party services
We use trusted providers to run parts of the service: Razorpay (payments — we never store your card details), Google (optional sign-in), Meta WhatsApp Business (OTPs and parent reports), Firebase (push notifications and analytics), SMS gateways, and AI providers. Each processes data under its own privacy policy, and we are not responsible for their independent practices.

# 6. Security — and its limits
We protect your data with encryption in transit, signed requests, access controls and access-limited storage. However, NO method of transmission or storage on the internet is 100% secure, and we cannot guarantee absolute security. To the maximum extent permitted by law, we are not liable for data loss, leaks or breaches caused by third parties, technical failure or events beyond our reasonable control. If a serious incident occurs, we will take reasonable remedial steps and notify affected users where the law requires.

# 7. Data retention & deletion
We keep your data while your account is active. You can delete your account anytime from Settings — your account is deactivated and excluded from the service, and data is removed or anonymised per our retention practices and legal obligations.

# 8. Children
Vaha AI is made for school students used with parental consent. A parent or guardian must accept our terms for users under 18 and may contact us to review or delete their child's data.

# 9. Your choices
You can edit your profile, change the parent WhatsApp number, turn notifications on or off, and delete your account in the app. For anything else, contact us.

# 10. Changes & contact
We may update this policy and will notify you of important changes in the app. Questions or requests: support@vahaai.com.`

// refundSeedContent is the default Refund Policy body.
const refundSeedContent = `Last updated: August 2026

# 1. Free trial
The free trial costs nothing. Cancel before it converts to a paid plan and you will never be charged.

# 2. Subscriptions are non-refundable
Once a subscription payment is charged, it is final and non-refundable, except as stated below. Cancelling stops FUTURE charges; you keep access until the end of the period already paid.

# 3. What we do refund
We refund: (a) duplicate payments for the same period; (b) amounts charged after a technical failure where the plan or credits were never activated. Approved refunds go back to the original payment method within 5-7 business days via Razorpay.

# 4. What we do not refund
We do not refund: dissatisfaction with AI-generated answers or voice (AI can make mistakes — see Terms & Conditions); partial or unused days; temporary downtime or feature changes; data loss or security incidents caused by third parties or events beyond our reasonable control; accounts suspended for violating our terms; credits already consumed by AI actions.

# 5. How to ask
Email support@vahaai.com from your registered account within 7 days of the charge, with the payment reference. We may ask for details to verify the claim, and our decision, made reasonably, is final to the maximum extent permitted by law.`

// SeedLegal inserts any missing default legal document (terms, privacy,
// refund). Idempotent per key — admin-edited documents are never touched.
func SeedLegal(db *gorm.DB) (bool, error) {
	docs := []model.LegalDocument{
		{Key: "terms", Title: "Terms & Conditions", Content: termsSeedContent},
		{Key: "privacy", Title: "Privacy Policy", Content: privacySeedContent},
		{Key: "refund", Title: "Refund Policy", Content: refundSeedContent},
	}
	added := false
	for _, doc := range docs {
		var count int64
		if err := db.Model(&model.LegalDocument{}).Where("key = ?", doc.Key).Count(&count).Error; err != nil {
			return added, err
		}
		if count > 0 {
			continue
		}
		if err := db.Create(&doc).Error; err != nil {
			return added, err
		}
		added = true
	}
	return added, nil
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

// EnsureStarterPlans backfills the paid tiers on a database that predates
// them. It only acts when NO paid plan exists at all: once the admin has
// curated the plan list (re-priced a tier, deleted one), re-inserting a
// missing name would resurrect a deliberately deleted plan on every boot.
func EnsureStarterPlans(db *gorm.DB) (int, error) {
	var count int64
	if err := db.Model(&model.Plan{}).Where("price_rupees > 0").Count(&count).Error; err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	plans := starterPaidPlans()
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
