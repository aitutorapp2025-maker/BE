package server

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/ai"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/cache"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/email"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/fcm"
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
	// No-expiry Redis cache for read-mostly config/master data (settings, plans,
	// teaching languages, class groups, landing). Each write busts its key(s).
	cacheStore := cache.NewStore(d.Redis)

	adminRepo := repository.NewAdminRepository(d.DB)
	studentRepo := repository.NewStudentRepository(d.DB)
	classRepo := repository.NewClassRepository(d.DB)
	classGroupRepo := repository.NewClassGroupRepository(d.DB, cacheStore)
	bookRepo := repository.NewBookRepository(d.DB)
	bookChunkRepo := repository.NewBookChunkRepository(d.DB)
	creditRepo := repository.NewCreditRepository(d.DB)
	planRepo := repository.NewPlanRepository(d.DB, cacheStore)
	settingRepo := repository.NewSettingRepository(d.DB, cacheStore)
	deviceTokenRepo := repository.NewDeviceTokenRepository(d.DB)
	legalRepo := repository.NewLegalRepository(d.DB)
	dashboardRepo := repository.NewDashboardRepository(d.DB)
	teachingLangRepo := repository.NewTeachingLanguageRepository(d.DB, cacheStore)

	// Landing content repositories.
	navRepo := repository.NewOrderedRepo[model.LandingNavItem](d.DB, cacheStore)
	statRepo := repository.NewOrderedRepo[model.LandingStat](d.DB, cacheStore)
	featureRepo := repository.NewOrderedRepo[model.LandingFeature](d.DB, cacheStore)
	testimonialRepo := repository.NewOrderedRepo[model.LandingTestimonial](d.DB, cacheStore)
	faqRepo := repository.NewOrderedRepo[model.LandingFaq](d.DB, cacheStore)
	landingTextRepo := repository.NewLandingTextRepo(d.DB, cacheStore)
	landingSeoRepo := repository.NewLandingSeoRepo(d.DB, cacheStore)
	contactRepo := repository.NewContactRepository(d.DB)

	emailPublisher := email.NewPublisher(d.MQ, func() bool { return d.SMTP().Enabled() })
	smsPublisher := sms.NewPublisher(d.MQ, func() bool { return d.SMS().Usable() })
	// AI config is read from the DB (admin Settings) with the env as fallback,
	// so keys can be changed at runtime.
	aiProvider := service.AIProvider(settingRepo, d.Cfg.AI)
	ingestPublisher := ai.NewIngestPublisher(d.MQ, func() bool { return aiProvider().Enabled() })
	// Tutoring (RAG) pipeline: Voyage embeddings + Claude answers. Keys are read
	// per call from aiProvider (DB settings → env fallback).
	chatClient := ai.NewChat(aiProvider)
	tutorService := service.NewTutorService(
		bookRepo, bookChunkRepo,
		ai.NewEmbedder(aiProvider, d.Redis),
		chatClient,
		d.Cfg.AI.TopK,
	)
	sessStore := session.New(d.Redis, d.Cfg.JWT.RefreshTTL)
	authService := service.NewAuthService(adminRepo, sessStore, d.Cfg)
	creditService := service.NewCreditService(creditRepo)
	// Homework: Claude vision reads an uploaded photo and splits it into tasks.
	// Images go to a PRIVATE dir served only via a signed /media route; AI actions
	// are metered on credits and uploads are rate-limited.
	homeworkService := service.NewHomeworkService(
		repository.NewHomeworkRepository(d.DB), studentRepo, chatClient, tutorService,
		d.Cfg.Uploads.PrivateDir, d.Cfg.Uploads.PublicBaseURL, d.Cfg.JWT.Secret)
	homeworkHandler := handler.NewHomeworkHandler(homeworkService, creditService, d.Redis)
	// Google SSO config comes from admin Settings (env client id as fallback).
	googleProvider := service.GoogleProvider(settingRepo, d.Cfg.GoogleClientID)
	// New students get their free trial (plan + credits) on first login.
	studentAuthService := service.NewStudentAuthService(
		studentRepo, deviceTokenRepo, sessStore, smsPublisher, d.Cfg, planRepo, creditService,
		googleProvider, settingRepo)

	healthHandler := handler.NewHealthHandler(d.DB, d.Redis, d.MQ)
	// Admin users & roles (RBAC): accounts are created with an emailed temporary
	// password; each role grants side-menu / settings-tab permission keys.
	adminRoleRepo := repository.NewAdminRoleRepository(d.DB)
	adminUserService := service.NewAdminUserService(
		adminRepo, adminRoleRepo, emailPublisher, d.Redis, d.Cfg.Uploads.PublicBaseURL)
	adminAuthHandler := handler.NewAdminAuthHandler(authService, adminRepo, adminUserService)
	adminUserHandler := handler.NewAdminUserHandler(adminUserService, adminRepo, adminRoleRepo)
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
		navRepo, statRepo, featureRepo, testimonialRepo, faqRepo, landingTextRepo, landingSeoRepo, settingRepo, cacheStore, chatClient, d.Redis)
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
	// Referral ("refer & earn"): mints share codes, attributes new signups, and
	// accrues the referrer's next-bill discount.
	referralRepo := repository.NewReferralRepository(d.DB)
	referralService := service.NewReferralService(studentRepo, referralRepo, settingRepo)
	referralHandler := handler.NewReferralHandler(referralService, referralRepo)
	studentAuthHandler := handler.NewStudentAuthHandler(studentAuthService, classGroupRepo, autopayEnabled, referralService, d.Payments, service.ProfilePasswordProvider(settingRepo))
	tutorHandler := handler.NewTutorHandler(tutorService, studentRepo, creditService, autopayEnabled)
	chatHistoryHandler := handler.NewChatHistoryHandler(
		repository.NewChatMessageRepository(d.DB), chatClient,
		d.Cfg.Uploads.Dir, d.Cfg.Uploads.PublicBaseURL)
	// Background push publisher (RabbitMQ) — shared by the support handler (notify
	// the student on an admin response) and the admin notification broadcast.
	pushPublisher := fcm.NewPublisher(d.MQ, func() bool { return d.Push.Enabled() })
	// "Report a problem" support tickets (student files/tracks; admin responds).
	supportRepo := repository.NewSupportRepository(d.DB)
	supportHandler := handler.NewSupportHandler(
		service.NewSupportService(supportRepo, d.Cfg.Uploads.PrivateDir,
			d.Cfg.Uploads.PublicBaseURL, d.Cfg.JWT.Secret),
		supportRepo, pushPublisher)
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

	// Referral short link — PUBLIC + plaintext (shared over WhatsApp; opened in a
	// browser, not the app), so it bypasses the E2E middleware. Detects the
	// device and redirects to the Play/App Store.
	app.Get("/r/:code", handler.NewReferralRedirectHandler(settingRepo).Redirect)

	v1 := app.Group("/api/v1")
	v1.Get("/ping", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"success": true, "message": "pong"})
	})

	// Anonymous E2E handshake so public endpoints can be encrypted too.
	v1.Post("/handshake", handshakeHandler.Handshake)

	// Audit trail: record every authenticated action (admin + student streams).
	// The recorder persists asynchronously so it never blocks the response.
	auditRepo := repository.NewAuditLogRepository(d.DB)
	// Buffer + batch audit writes so a burst of requests becomes a few multi-row
	// INSERTs instead of one goroutine+INSERT each. Enqueue is non-blocking.
	auditBuffer := repository.NewAuditBuffer(auditRepo)
	auditRecord := func(e middleware.AuditEntry) {
		auditBuffer.Enqueue(model.AuditLog{
			ActorType:    e.ActorType,
			ActorID:      e.ActorID,
			ActorLabel:   e.ActorLabel,
			Method:       e.Method,
			Path:         e.Path,
			Query:        e.Query,
			Status:       e.Status,
			IP:           e.IP,
			UserAgent:    e.UserAgent,
			LatencyMs:    e.LatencyMs,
			RequestBody:  e.RequestBody,
			ResponseBody: e.ResponseBody,
		})
	}
	audit := middleware.Audit(auditRecord)

	// Public endpoints — encrypted for clients that completed the anon handshake
	// (the Encrypt middleware reads the X-Session header).
	enc := middleware.Encrypt(sessStore)
	v1.Get("/landing", enc, landingHandler.Public)
	// Public website chat assistant (rate-limited per IP; anon-E2E encrypted).
	v1.Post("/landing/chat", enc, landingHandler.Chat)
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
	// Home-screen banners (admin-managed image/text promos) for the app.
	bannerHandler := handler.NewHomeBannerHandler(
		repository.NewHomeBannerRepository(d.DB, cacheStore))
	v1.Get("/banners", enc, bannerHandler.Public)
	// Maintenance status for the customer apps (web + mobile). Admin unaffected.
	v1.Get("/maintenance", enc, settingHandler.Maintenance)
	// Which sign-in methods to show on the login screen (Google SSO, …).
	v1.Get("/auth-config", enc, handler.NewAuthConfigHandler(googleProvider, settingRepo).Get)
	// Mobile app version / force-update check.
	v1.Get("/app-version", enc, settingHandler.AppVersion)
	// Signed access to private homework images + support attachments (not on the
	// public /uploads mount).
	mediaHandler := handler.NewMediaHandler(d.Cfg.Uploads.PrivateDir, d.Cfg.JWT.Secret)
	v1.Get("/media/hw/:file", mediaHandler.Homework)
	v1.Get("/media/support/:file", mediaHandler.Support)

	// Student (mobile) passwordless login — OTP over SMS. Encrypted end-to-end
	// like the other public endpoints (phone number + code stay opaque).
	student := v1.Group("/student")
	// Register the FCM token at app open (before login) — stored unmapped.
	student.Post("/register-device", enc, studentAuthHandler.RegisterDevice)
	student.Post("/send-otp", enc, studentAuthHandler.SendOTP)
	student.Post("/verify-otp", enc, studentAuthHandler.VerifyOTP)
	// Google (Gmail) SSO — verifies the Google ID token, opens a session.
	student.Post("/google", enc, studentAuthHandler.GoogleLogin)
	// Streaming (SSE) endpoints: words appear as the tutor generates them. Signed
	// (JWT + nonce + HMAC) and end-to-end encrypted — the request body is
	// decrypted by DecryptRequest and each SSE frame is an AES-GCM envelope (see
	// sseStream). Not audited/Encrypt'd: an SSE response can't pass the buffering
	// Audit/Encrypt middleware.
	//
	// Registered with PER-ROUTE middleware and BEFORE studentProtected — NOT via
	// student.Group("", ...). An empty-prefix group registers a `Use` that leaks
	// its middleware onto every /student route declared afterwards; that was
	// double-applying SignedStudent (→ nonce replay on GETs, and a body-hash
	// mismatch on POSTs once Encrypt had already decrypted the body) and wrongly
	// wrapping the SSE routes in Encrypt.
	sseSigned := middleware.SignedStudent(d.Cfg, sessStore)
	sseDecrypt := middleware.DecryptRequest(sessStore)
	student.Post("/ask/stream", sseSigned, sseDecrypt, tutorHandler.AskStream)
	student.Post("/homework/:id/tasks/:taskId/teach/stream", sseSigned, sseDecrypt, homeworkHandler.TeachStream)
	student.Post("/homework/:id/tasks/:taskId/doubt/stream", sseSigned, sseDecrypt, homeworkHandler.DoubtStream)
	student.Post("/homework/:id/test/grade/stream", sseSigned, sseDecrypt, homeworkHandler.GradeTestStream)

	// Signed-in student endpoints (profile + device token) — protected by the
	// same strong scheme as admin: signed request (JWT + one-time nonce + HMAC
	// signature) with end-to-end encrypted payloads.
	studentProtected := student.Group("",
		middleware.SignedStudent(d.Cfg, sessStore),
		middleware.Encrypt(sessStore),
		audit)
	studentProtected.Get("/me", studentAuthHandler.Me)
	studentProtected.Put("/profile", studentAuthHandler.UpdateProfile)
	// Refer & earn: the student's own code, share link and reward status.
	studentProtected.Get("/referral", referralHandler.Me)
	// Notification feed (broadcasts + targeted) for the bell + unread count.
	studentProtected.Get("/notifications",
		handler.NewNotificationFeedHandler(repository.NewNotificationRepository(d.DB)).List)
	// The student's own payment history (Billing screen).
	studentProtected.Get("/payments",
		handler.NewStudentPaymentHandler(creditRepo).List)
	// Soft-delete the account (kept for audit, hidden everywhere; re-register
	// starts fresh).
	studentProtected.Delete("/account", studentAuthHandler.DeleteAccount)
	studentProtected.Post("/device-token", studentAuthHandler.SaveDeviceToken)
	// Notifications on/off (Profile & Settings toggle) for this student's devices.
	studentProtected.Post("/notifications/enabled", studentAuthHandler.SetNotifications)
	// Ask the AI tutor a textbook question (retrieval + Claude answer).
	studentProtected.Post("/ask", tutorHandler.Ask)
	// AI-tutor chat history — synced across devices (stored in Postgres).
	studentProtected.Get("/chat", chatHistoryHandler.List)
	studentProtected.Post("/chat/sync", chatHistoryHandler.Sync)
	// Voice notes: store the audio + transcribe it (WhatsApp-style chat).
	studentProtected.Post("/chat/file", chatHistoryHandler.File)
	studentProtected.Post("/chat/voice", chatHistoryHandler.Voice)
	// Multi-chat: named conversation threads (New chat / history / rename).
	studentProtected.Get("/chat/conversations", chatHistoryHandler.Conversations)
	studentProtected.Post("/chat/conversations/sync", chatHistoryHandler.SyncConversations)
	studentProtected.Delete("/chat/conversations/:convId", chatHistoryHandler.DeleteConversation)
	// Report a problem: file a ticket (+ optional attachment) and track it.
	studentProtected.Post("/support", supportHandler.Create)
	studentProtected.Get("/support", supportHandler.ListMine)
	// Homework: upload a photo → AI reads it and splits it into tasks.
	studentProtected.Post("/homework", homeworkHandler.Upload)
	studentProtected.Get("/homework", homeworkHandler.List)
	// Delta sync for local-first history (Drift/SQLite on the device).
	studentProtected.Post("/sync", homeworkHandler.Sync)
	// Performance report (marks + progress, date-wise; skip-aware).
	studentProtected.Get("/report", homeworkHandler.Report)
	// Mark a task done/skipped (execute one by one; skippable stages).
	studentProtected.Put("/homework/:id/tasks/:taskId", homeworkHandler.SetTaskStatus)
	// Move a task's study time (re-arms its "time to study" push reminder).
	studentProtected.Put("/homework/:id/tasks/:taskId/schedule", homeworkHandler.RescheduleTask)
	// Teach one task (RAG lesson in the student's language).
	studentProtected.Post("/homework/:id/tasks/:taskId/teach", homeworkHandler.Teach)
	// Clear a doubt about one task (grounded Q&A in the student's language).
	studentProtected.Post("/homework/:id/tasks/:taskId/doubt", homeworkHandler.Doubt)
	// Written test: generate questions, then grade typed or handwritten answers.
	studentProtected.Post("/homework/:id/test", homeworkHandler.GenerateTest)
	studentProtected.Post("/homework/:id/test/grade", homeworkHandler.GradeTest)
	studentProtected.Get("/homework/:id", homeworkHandler.Get)
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
	// Forgot/reset password — public like login (no session yet), anon-E2E so
	// the email/code/new password stay opaque on the wire.
	admin.Post("/forgot-password", enc, adminAuthHandler.ForgotPassword)
	admin.Post("/reset-password", enc, adminAuthHandler.ResetPassword)

	// Protected admin routes require a signed request: valid JWT + timestamp +
	// HMAC signature + a one-time nonce (see middleware.SignedAdmin). The Encrypt
	// middleware then transparently decrypts requests / encrypts responses for
	// sessions that completed the E2E key exchange. AdminPermission then enforces
	// the signed-in admin's role permissions per route (super admins pass all).
	adminProtected := admin.Group("",
		middleware.SignedAdmin(d.Cfg, sessStore),
		middleware.Encrypt(sessStore),
		audit,
		middleware.AdminPermission(adminRepo))
	adminProtected.Get("/me", adminAuthHandler.Me)
	adminProtected.Post("/logout", adminAuthHandler.Logout)
	adminProtected.Post("/change-password", adminAuthHandler.ChangePassword)

	// Admin users & roles (RBAC) — gated on the admin_users permission.
	adminUsers := adminProtected.Group("/admins")
	adminUsers.Get("", adminUserHandler.List)
	adminUsers.Post("", adminUserHandler.Create)
	adminUsers.Put("/:id", adminUserHandler.Update)
	adminUsers.Delete("/:id", adminUserHandler.Delete)
	adminUsers.Post("/:id/reset-password", adminUserHandler.ResetPassword)
	adminRoles := adminProtected.Group("/roles")
	adminRoles.Get("", adminUserHandler.ListRoles)
	adminRoles.Post("", adminUserHandler.CreateRole)
	adminRoles.Put("/:id", adminUserHandler.UpdateRole)
	adminRoles.Delete("/:id", adminUserHandler.DeleteRole)
	adminProtected.Get("/permissions", adminUserHandler.Permissions)

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

	// Push notifications — admin broadcast to all customers or selected students,
	// queued on RabbitMQ and delivered by the push worker (pushPublisher built above).
	notificationHandler := handler.NewNotificationHandler(pushPublisher)
	// Audit logs — admin views the admin + student activity streams; the detail
	// route returns one entry's full request/response payloads.
	auditLogHandler := handler.NewAuditLogHandler(auditRepo)
	adminProtected.Get("/audit-logs", auditLogHandler.List)
	adminProtected.Get("/audit-logs/:id", auditLogHandler.Get)
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

	// Home-screen banners CRUD (+ image upload).
	adminBanners := adminProtected.Group("/banners")
	adminBanners.Get("", bannerHandler.List)
	adminBanners.Post("", bannerHandler.Create)
	adminBanners.Post("/image", uploadHandler.UploadBannerImage)
	adminBanners.Put("/:id", bannerHandler.Update)
	adminBanners.Delete("/:id", bannerHandler.Delete)

	// Settings (singleton).
	adminProtected.Get("/settings", settingHandler.Get)
	adminProtected.Put("/settings", settingHandler.Update)

	// Firebase Analytics + Crashlytics dashboards (synced daily from BigQuery;
	// "Sync now" is queued on RabbitMQ and run by the firebase-sync worker).
	fbStatsRepo := repository.NewFirebaseStatsRepository(d.DB)
	fbStatsHandler := handler.NewFirebaseStatsHandler(fbStatsRepo, settingRepo, d.MQ)
	adminProtected.Get("/analytics", fbStatsHandler.Analytics)
	adminProtected.Get("/crashlytics", fbStatsHandler.Crashlytics)
	adminProtected.Post("/firebase-stats/sync", fbStatsHandler.Sync)

	// "Report a problem" support tickets — list + respond.
	adminProtected.Get("/support", supportHandler.AdminList)
	adminProtected.Put("/support/:id", supportHandler.AdminReply)
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

	// Referrals — admin view of who referred whom (refer & earn).
	adminProtected.Get("/referrals", referralHandler.List)

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
