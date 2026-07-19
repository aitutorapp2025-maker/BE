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

	// ── Public routes ────────────────────────────────────────────────────
	app.Get("/health", healthHandler.Check)

	v1 := app.Group("/api/v1")
	v1.Get("/ping", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true, "message": "pong"})
	})

	// Public landing-page content (consumed by the marketing site).
	v1.Get("/landing", landingHandler.Public)

	// Public "Get in touch" submission.
	v1.Post("/contact", contactHandler.Submit)

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
	admin.Post("/login", adminAuthHandler.Login)
	// Refresh is authenticated by the (single-use) refresh token itself, so it
	// is not signature-protected (the access token may be expired here).
	admin.Post("/refresh", adminAuthHandler.Refresh)

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
