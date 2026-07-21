package server

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/handler"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/middleware"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
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
	bookRepo := repository.NewBookRepository(d.DB)
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
	contactRepo := repository.NewContactRepository(d.DB)

	emailPublisher := email.NewPublisher(d.MQ, func() bool { return d.SMTP().Enabled() })
	smsPublisher := sms.NewPublisher(d.MQ, func() bool { return d.SMS().Usable() })
	sessStore := session.New(d.Redis, d.Cfg.JWT.RefreshTTL)
	authService := service.NewAuthService(adminRepo, sessStore, d.Cfg)
	studentAuthService := service.NewStudentAuthService(studentRepo, deviceTokenRepo, sessStore, smsPublisher, d.Cfg)

	healthHandler := handler.NewHealthHandler(d.DB, d.Redis, d.MQ)
	adminAuthHandler := handler.NewAdminAuthHandler(authService, adminRepo)
	studentHandler := handler.NewStudentHandler(studentRepo)
	classHandler := handler.NewClassHandler(classRepo)
	bookHandler := handler.NewBookHandler(bookRepo)
	planHandler := handler.NewPlanHandler(planRepo)
	settingHandler := handler.NewSettingHandler(settingRepo, emailPublisher, smsPublisher)

	landingHandler := handler.NewLandingHandler(
		navRepo, statRepo, featureRepo, testimonialRepo, faqRepo, landingTextRepo, settingRepo)
	navCrud := handler.NewLandingCrudHandler[model.LandingNavItem, *model.LandingNavItem](navRepo, "nav item")
	statCrud := handler.NewLandingCrudHandler[model.LandingStat, *model.LandingStat](statRepo, "stat")
	featureCrud := handler.NewLandingCrudHandler[model.LandingFeature, *model.LandingFeature](featureRepo, "feature")
	testimonialCrud := handler.NewLandingCrudHandler[model.LandingTestimonial, *model.LandingTestimonial](testimonialRepo, "testimonial")
	faqCrud := handler.NewLandingCrudHandler[model.LandingFaq, *model.LandingFaq](faqRepo, "faq")
	landingTextHandler := handler.NewLandingTextHandler(landingTextRepo)
	contactHandler := handler.NewContactHandler(contactRepo, settingRepo, emailPublisher, smsPublisher, d.Log)
	handshakeHandler := handler.NewHandshakeHandler(sessStore)
	studentAuthHandler := handler.NewStudentAuthHandler(studentAuthService)
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
	v1.Post("/contact", enc, contactHandler.Submit)
	// Legal documents (Terms & Conditions, etc.) shown in the app.
	v1.Get("/legal/:key", enc, legalHandler.Public)
	// Active teaching languages for the app profile screen.
	v1.Get("/teaching-languages", enc, teachingLangHandler.Public)

	// Student (mobile) passwordless login — OTP over SMS. Encrypted end-to-end
	// like the other public endpoints (phone number + code stay opaque).
	student := v1.Group("/student")
	// Register the FCM token at app open (before login) — stored unmapped.
	student.Post("/register-device", enc, studentAuthHandler.RegisterDevice)
	student.Post("/send-otp", enc, studentAuthHandler.SendOTP)
	student.Post("/verify-otp", enc, studentAuthHandler.VerifyOTP)
	// Signed-in student endpoints (profile + device token) — protected by the
	// same strong scheme as admin: signed request (JWT + one-time nonce + HMAC
	// signature) with end-to-end encrypted payloads.
	studentProtected := student.Group("",
		middleware.SignedStudent(d.Cfg, sessStore),
		middleware.Encrypt(sessStore))
	studentProtected.Get("/me", studentAuthHandler.Me)
	studentProtected.Put("/profile", studentAuthHandler.UpdateProfile)
	studentProtected.Post("/device-token", studentAuthHandler.SaveDeviceToken)

	// Public client-side error reporting (emails an alert to the admin).
	errorReportHandler := handler.NewErrorReportHandler(d.Alerter)
	v1.Post("/errors", errorReportHandler.Report)

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

	// Students CRUD.
	students := adminProtected.Group("/students")
	students.Get("", studentHandler.List)
	students.Post("", studentHandler.Create)
	students.Get("/:id", studentHandler.Get)
	students.Put("/:id", studentHandler.Update)
	students.Delete("/:id", studentHandler.Delete)

	// Classes CRUD.
	classes := adminProtected.Group("/classes")
	classes.Get("", classHandler.List)
	classes.Post("", classHandler.Create)
	classes.Get("/:id", classHandler.Get)
	classes.Put("/:id", classHandler.Update)
	classes.Delete("/:id", classHandler.Delete)

	// Books CRUD (list supports ?class_name= & ?medium=).
	books := adminProtected.Group("/books")
	books.Get("", bookHandler.List)
	books.Post("", bookHandler.Create)
	books.Get("/:id", bookHandler.Get)
	books.Put("/:id", bookHandler.Update)
	books.Delete("/:id", bookHandler.Delete)

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
	adminProtected.Post("/settings/test-email", settingHandler.TestEmail)
	adminProtected.Post("/settings/test-sms", settingHandler.TestSMS)

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
