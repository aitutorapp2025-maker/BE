package server

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/handler"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/middleware"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/payment"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/session"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/sms"
	"github.com/gofiber/fiber/v2"
)

// registerRoutes mounts all HTTP routes. Feature routes are added here as the
// API grows (auth, students, plans, classes, books, ...).
func registerRoutes(app *fiber.App, d Deps) {
	// ── Dependency wiring ────────────────────────────────────────────────
	adminRepo := repository.NewAdminRepository(d.DB)
	studentRepo := repository.NewStudentRepository(d.DB)
	classRepo := repository.NewClassRepository(d.DB)
	classGroupRepo := repository.NewClassGroupRepository(d.DB)
	bookRepo := repository.NewBookRepository(d.DB)
	bookChunkRepo := repository.NewBookChunkRepository(d.DB)
	creditRepo := repository.NewCreditRepository(d.DB)
	planRepo := repository.NewPlanRepository(d.DB)
	settingRepo := repository.NewSettingRepository(d.DB)
	deviceTokenRepo := repository.NewDeviceTokenRepository(d.DB)
	legalRepo := repository.NewLegalRepository(d.DB)
	dashboardRepo := repository.NewDashboardRepository(d.DB)
	teachingLangRepo := repository.NewTeachingLanguageRepository(d.DB)

	// Landing content repositories.
	navRepo := repository.NewOrderedRepo[model.LandingNavItem](d.DB)
	statRepo := repository.NewOrderedRepo[model.LandingStat](d.DB)
	featureRepo := repository.NewOrderedRepo[model.LandingFeature](d.DB)
	testimonialRepo := repository.NewOrderedRepo[model.LandingTestimonial](d.DB)
	faqRepo := repository.NewOrderedRepo[model.LandingFaq](d.DB)
	landingTextRepo := repository.NewLandingTextRepo(d.DB)
	landingSeoRepo := repository.NewLandingSeoRepo(d.DB)
	contactRepo := repository.NewContactRepository(d.DB)

	emailPublisher := email.NewPublisher(d.MQ, func() bool { return d.SMTP().Enabled() })
	smsPublisher := sms.NewPublisher(d.MQ, func() bool { return d.SMS().Usable() })
	// AI config is read from the DB (admin Settings) with the env as fallback,
	// so keys can be changed at runtime.
	aiProvider := service.AIProvider(settingRepo, d.Cfg.AI)
	ingestPublisher := ai.NewIngestPublisher(d.MQ, func() bool { return aiProvider().Enabled() })
	// Tutoring (RAG) pipeline: Voyage embeddings + Claude answers. Keys are read
	// per call from aiProvider (DB settings → env fallback).
	tutorService := service.NewTutorService(
		bookRepo, bookChunkRepo,
		ai.NewEmbedder(aiProvider),
		ai.NewChat(aiProvider),
		d.Cfg.AI.TopK,
	)
	sessStore := session.New(d.Redis, d.Cfg.JWT.RefreshTTL)
	authService := service.NewAuthService(adminRepo, sessStore, d.Cfg)
	creditService := service.NewCreditService(creditRepo)
	// Google SSO config comes from admin Settings (env client id as fallback).
	googleProvider := service.GoogleProvider(settingRepo, d.Cfg.GoogleClientID)
	// New students get their free trial (plan + credits) on first login.
	studentAuthService := service.NewStudentAuthService(
		studentRepo, deviceTokenRepo, sessStore, smsPublisher, d.Cfg, planRepo, creditService,
		googleProvider)

	healthHandler := handler.NewHealthHandler(d.DB, d.Redis, d.MQ)
	adminAuthHandler := handler.NewAdminAuthHandler(authService, adminRepo)
	studentHandler := handler.NewStudentHandler(studentRepo)
	classHandler := handler.NewClassHandler(classRepo, classGroupRepo)
	classGroupHandler := handler.NewClassGroupHandler(classGroupRepo)
	bookHandler := handler.NewBookHandler(bookRepo, ingestPublisher)
	// Razorpay keys come from admin Settings (env fallback), read per call.
	razorpayProvider := service.RazorpayProvider(settingRepo, d.Cfg.Razorpay)
	razorpayClient := payment.NewClient(razorpayProvider)
	// The plan handler auto-creates a Razorpay plan on create/price-change.
	planHandler := handler.NewPlanHandler(planRepo, razorpayClient)
	settingHandler := handler.NewSettingHandler(settingRepo, emailPublisher, smsPublisher, tutorService.Probe)

	landingHandler := handler.NewLandingHandler(
		navRepo, statRepo, featureRepo, testimonialRepo, faqRepo, landingTextRepo, landingSeoRepo, settingRepo)
	navCrud := handler.NewLandingCrudHandler[model.LandingNavItem, *model.LandingNavItem](navRepo, "nav item")
	statCrud := handler.NewLandingCrudHandler[model.LandingStat, *model.LandingStat](statRepo, "stat")
	featureCrud := handler.NewLandingCrudHandler[model.LandingFeature, *model.LandingFeature](featureRepo, "feature")
	testimonialCrud := handler.NewLandingCrudHandler[model.LandingTestimonial, *model.LandingTestimonial](testimonialRepo, "testimonial")
	faqCrud := handler.NewLandingCrudHandler[model.LandingFaq, *model.LandingFaq](faqRepo, "faq")
	landingTextHandler := handler.NewLandingTextHandler(landingTextRepo)
	landingSeoHandler := handler.NewLandingSeoHandler(landingSeoRepo)
	uploadHandler := handler.NewAssetUploadHandler(
		d.Cfg.Uploads.Dir, d.Cfg.Uploads.PublicBaseURL)
	contactHandler := handler.NewContactHandler(contactRepo, settingRepo, emailPublisher, smsPublisher, d.Log)
	handshakeHandler := handler.NewHandshakeHandler(sessStore)
	// Whether trials require a UPI-AutoPay mandate — admin-toggleable, read per call.
	autopayEnabled := service.AutopayProvider(settingRepo)
	studentAuthHandler := handler.NewStudentAuthHandler(studentAuthService, classGroupRepo, autopayEnabled)
	tutorHandler := handler.NewTutorHandler(tutorService, studentRepo, creditService, autopayEnabled)
	creditHandler := handler.NewCreditHandler(creditService, studentRepo)
	reportHandler := handler.NewReportHandler(studentRepo, creditRepo)

	// Razorpay UPI-AutoPay: reuse the PaymentService built in main.go (shared with
	// the recurring-charge scheduler job); fall back to building one if absent.
	paymentService := d.Payments
	if paymentService == nil {
		paymentService = service.NewPaymentService(
			razorpayClient, razorpayProvider,
			studentRepo, planRepo, creditService,
			repository.NewPaymentEventRepository(d.DB))
	}
	paymentHandler := handler.NewPaymentHandler(paymentService, d.Log)
	legalHandler := handler.NewLegalHandler(legalRepo)
	dashboardHandler := handler.NewDashboardHandler(dashboardRepo)
	teachingLangHandler := handler.NewTeachingLanguageHandler(teachingLangRepo)

	// ── Public routes ────────────────────────────────────────────────────
	app.Get("/health", healthHandler.Check)

	v1 := app.Group("/api/v1")
	v1.Get("/ping", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true, "message": "pong"})
	})

	// Anonymous E2E handshake so public endpoints can be encrypted too.
	v1.Post("/handshake", handshakeHandler.Handshake)

	// Public endpoints — encrypted for clients that completed the anon handshake
	// (the Encrypt middleware reads the X-Session header).
	enc := middleware.Encrypt(sessStore)
	v1.Get("/landing", enc, landingHandler.Public)
	// Plain (not E2E) SEO head fragment for hosting-layer injection into
	// index.html so no-JS social crawlers get the admin-managed meta.
	v1.Get("/landing/meta.html", landingSeoHandler.MetaHTML)
	v1.Post("/contact", enc, contactHandler.Submit)
	// Legal documents (Terms & Conditions, etc.) shown in the app.
	v1.Get("/legal/:key", enc, legalHandler.Public)
	// Active teaching languages for the app profile screen.
	v1.Get("/teaching-languages", enc, teachingLangHandler.Public)
	// Subject groups (11 & 12 streams) for the app profile screen — optionally
	// filtered with ?class=&board=.
	v1.Get("/class-groups", enc, classGroupHandler.Public)
	// Subscription plans for the landing page + student app.
	v1.Get("/plans", enc, planHandler.Public)
	// Maintenance status for the customer apps (web + mobile). Admin unaffected.
	v1.Get("/maintenance", enc, settingHandler.Maintenance)
	// Which sign-in methods to show on the login screen (Google SSO, …).
	v1.Get("/auth-config", enc, handler.NewAuthConfigHandler(googleProvider).Get)
	// Mobile app version / force-update check.
	v1.Get("/app-version", enc, settingHandler.AppVersion)

	// Student (mobile) passwordless login — OTP over SMS. Encrypted end-to-end
	// like the other public endpoints (phone number + code stay opaque).
	student := v1.Group("/student")
	// Register the FCM token at app open (before login) — stored unmapped.
	student.Post("/register-device", enc, studentAuthHandler.RegisterDevice)
	student.Post("/send-otp", enc, studentAuthHandler.SendOTP)
	student.Post("/verify-otp", enc, studentAuthHandler.VerifyOTP)
	// Google (Gmail) SSO — verifies the Google ID token, opens a session.
	student.Post("/google", enc, studentAuthHandler.GoogleLogin)
	// Signed-in student endpoints (profile + device token) — protected by the
	// same strong scheme as admin: signed request (JWT + one-time nonce + HMAC
	// signature) with end-to-end encrypted payloads.
	studentProtected := student.Group("",
		middleware.SignedStudent(d.Cfg, sessStore),
		middleware.Encrypt(sessStore))
	studentProtected.Get("/me", studentAuthHandler.Me)
	studentProtected.Put("/profile", studentAuthHandler.UpdateProfile)
	// Soft-delete the account (kept for audit, hidden everywhere; re-register
	// starts fresh).
	studentProtected.Delete("/account", studentAuthHandler.DeleteAccount)
	studentProtected.Post("/device-token", studentAuthHandler.SaveDeviceToken)
	// Ask the AI tutor a textbook question (retrieval + Claude answer).
	studentProtected.Post("/ask", tutorHandler.Ask)
	// Start a UPI-AutoPay subscription (returns the Razorpay checkout link).
	studentProtected.Post("/subscribe", paymentHandler.Subscribe)
	// Headless UPI-AutoPay: returns a GPay intent deeplink (no Razorpay UI).
	studentProtected.Post("/mandate-intent", paymentHandler.MandateIntent)

	// Public client-side error reporting (emails an alert to the admin).
	errorReportHandler := handler.NewErrorReportHandler(d.Alerter)
	v1.Post("/errors", errorReportHandler.Report)

	// Razorpay subscription webhook — PUBLIC + plaintext by necessity (called by
	// Razorpay's servers, which can't do our E2E handshake). Authenticated by the
	// HMAC-SHA256 signature in X-Razorpay-Signature, verified in the handler.
	v1.Post("/payments/webhook", paymentHandler.Webhook)

	// Dev-only: simulate a server error to test error alerting.
	if !d.Cfg.IsProduction() {
		v1.Get("/debug/boom", func(c *fiber.Ctx) error {
			return fiber.NewError(fiber.StatusInternalServerError, "boom: simulated server error")
		})
	}

	// ── Admin ────────────────────────────────────────────────────────────
	admin := v1.Group("/admin")
	// Login + refresh are end-to-end encrypted via the anonymous handshake key
	// (no session key exists yet), so credentials + tokens stay opaque on the
	// wire. They are not signature-protected (no session/secret yet).
	admin.Post("/login", enc, adminAuthHandler.Login)
	admin.Post("/refresh", enc, adminAuthHandler.Refresh)

	// Protected admin routes require a signed request: valid JWT + timestamp +
	// HMAC signature + a one-time nonce (see middleware.SignedAdmin). The Encrypt
	// middleware then transparently decrypts requests / encrypts responses for
	// sessions that completed the E2E key exchange.
	adminProtected := admin.Group("",
		middleware.SignedAdmin(d.Cfg, sessStore),
		middleware.Encrypt(sessStore))
	adminProtected.Get("/me", adminAuthHandler.Me)
	adminProtected.Post("/logout", adminAuthHandler.Logout)
	adminProtected.Post("/change-password", adminAuthHandler.ChangePassword)

	// Dashboard overview (aggregate stats + recent students).
	adminProtected.Get("/dashboard", dashboardHandler.Get)

	// System status — Postgres / Redis / RabbitMQ health for the settings page.
	adminProtected.Get("/status", healthHandler.Services)

	// Billing / profit & loss (revenue vs AI cost — admin only).
	adminProtected.Get("/billing", creditHandler.PnL)

	// Cron jobs (background jobs the admin can enable/disable).
	cronHandler := handler.NewCronHandler(repository.NewCronRepository(d.DB), d.Sched)
	crons := adminProtected.Group("/crons")
	crons.Get("", cronHandler.List)
	crons.Put("/:key", cronHandler.Toggle)
	crons.Post("/:key/run", cronHandler.Run)

	// Push notifications — admin broadcast to all customers or selected students.
	notificationHandler := handler.NewNotificationHandler(
		repository.NewDeviceTokenRepository(d.DB), d.Push)
	adminProtected.Post("/notifications/send", notificationHandler.Send)
	adminProtected.Post("/notifications/image", uploadHandler.UploadNotificationImage)

	// Reports (CSV exports, Excel-openable).
	reports := adminProtected.Group("/reports")
	reports.Get("/students", reportHandler.Students)
	reports.Get("/payments", reportHandler.Payments)
	reports.Get("/summary", reportHandler.Summary)

	// Students CRUD.
	students := adminProtected.Group("/students")
	students.Get("", studentHandler.List)
	students.Post("", studentHandler.Create)
	students.Get("/:id", studentHandler.Get)
	students.Put("/:id", studentHandler.Update)
	students.Delete("/:id", studentHandler.Delete)
	// Credits: top up a student and view their credit history.
	students.Post("/:id/recharge", creditHandler.Recharge)
	students.Get("/:id/ledger", creditHandler.Ledger)

	// Classes CRUD.
	classes := adminProtected.Group("/classes")
	classes.Get("", classHandler.List)
	classes.Post("", classHandler.Create)
	classes.Get("/:id", classHandler.Get)
	classes.Put("/:id", classHandler.Update)
	classes.Delete("/:id", classHandler.Delete)

	// Class groups — the subject streams offered for a class + board (11 & 12).
	// List supports ?class= to scope it to one class.
	classGroups := adminProtected.Group("/class-groups")
	classGroups.Get("", classGroupHandler.List)
	classGroups.Post("", classGroupHandler.Create)
	classGroups.Put("/:id", classGroupHandler.Update)
	classGroups.Delete("/:id", classGroupHandler.Delete)

	// Books CRUD (list supports ?class_name= & ?medium=).
	books := adminProtected.Group("/books")
	books.Get("", bookHandler.List)
	books.Post("", bookHandler.Create)
	books.Get("/:id", bookHandler.Get)
	books.Put("/:id", bookHandler.Update)
	books.Delete("/:id", bookHandler.Delete)
	// Index a book's text into the vector store for retrieval.
	books.Post("/:id/ingest", bookHandler.Ingest)

	// Plans CRUD.
	plans := adminProtected.Group("/plans")
	plans.Get("", planHandler.List)
	plans.Post("", planHandler.Create)
	plans.Get("/:id", planHandler.Get)
	plans.Put("/:id", planHandler.Update)
	plans.Delete("/:id", planHandler.Delete)

	// Settings (singleton).
	adminProtected.Get("/settings", settingHandler.Get)
	adminProtected.Put("/settings", settingHandler.Update)
	adminProtected.Post("/settings/logo", uploadHandler.UploadLogo)
	adminProtected.Post("/settings/test-email", settingHandler.TestEmail)
	adminProtected.Post("/settings/test-sms", settingHandler.TestSMS)
	adminProtected.Post("/settings/test-ai", settingHandler.TestAI)

	// Legal documents (Terms & Conditions, etc.) — admin editing.
	adminProtected.Get("/legal/:key", legalHandler.Get)
	adminProtected.Put("/legal/:key", legalHandler.Update)

	// Teaching languages master (admin CRUD).
	langs := adminProtected.Group("/teaching-languages")
	langs.Get("", teachingLangHandler.List)
	langs.Post("", teachingLangHandler.Create)
	langs.Put("/:id", teachingLangHandler.Update)
	langs.Delete("/:id", teachingLangHandler.Delete)

	// Contact submissions (enquiries).
	contacts := adminProtected.Group("/contacts")
	contacts.Get("", contactHandler.List)
	contacts.Get("/:id", contactHandler.Get)
	contacts.Delete("/:id", contactHandler.Delete)

	// Landing-page CMS (admin editing).
	landing := adminProtected.Group("/landing")
	landing.Get("/text", landingTextHandler.Get)
	landing.Put("/text", landingTextHandler.Update)
	landing.Get("/seo", landingSeoHandler.Get)
	landing.Put("/seo", landingSeoHandler.Update)
	landing.Post("/seo/og-image", uploadHandler.UploadOgImage)
	registerLandingCrud(landing, "nav", navCrud.List, navCrud.Create, navCrud.Update, navCrud.Delete)
	registerLandingCrud(landing, "stats", statCrud.List, statCrud.Create, statCrud.Update, statCrud.Delete)
	registerLandingCrud(landing, "features", featureCrud.List, featureCrud.Create, featureCrud.Update, featureCrud.Delete)
	registerLandingCrud(landing, "testimonials", testimonialCrud.List, testimonialCrud.Create, testimonialCrud.Update, testimonialCrud.Delete)
	registerLandingCrud(landing, "faqs", faqCrud.List, faqCrud.Create, faqCrud.Update, faqCrud.Delete)
}

// registerLandingCrud mounts the standard 5 CRUD routes for a landing list entity.
func registerLandingCrud(g fiber.Router, path string, list, create, update, del fiber.Handler) {
	sub := g.Group("/" + path)
	sub.Get("", list)
	sub.Post("", create)
	sub.Put("/:id", update)
	sub.Delete("/:id", del)
}
