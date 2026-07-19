package database

import (
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"gorm.io/gorm"
)

// SeedLanding populates the landing-page content tables with the current
// default copy, once, if they are empty. Returns true if anything was seeded.
func SeedLanding(db *gorm.DB) (bool, error) {
	seeded := false

	// Nav items.
	if n, err := seedIfEmpty(db, &model.LandingNavItem{}, func() error {
		nav := []model.LandingNavItem{
			{Label: "Features", SectionKey: "features", Enabled: true, SortOrder: 1},
			{Label: "How it works", SectionKey: "how", Enabled: true, SortOrder: 2},
			{Label: "Pricing", SectionKey: "pricing", Enabled: true, SortOrder: 3},
			{Label: "Reviews", SectionKey: "reviews", Enabled: true, SortOrder: 4},
			{Label: "FAQ", SectionKey: "faq", Enabled: true, SortOrder: 5},
			{Label: "Contact", SectionKey: "contact", Enabled: true, SortOrder: 6},
		}
		return db.Create(&nav).Error
	}); err != nil {
		return false, err
	} else if n {
		seeded = true
	}

	// Stats.
	if n, err := seedIfEmpty(db, &model.LandingStat{}, func() error {
		stats := []model.LandingStat{
			{Value: "10,000+", Label: "Students learning", SortOrder: 1},
			{Value: "50,000+", Label: "Homeworks explained", SortOrder: 2},
			{Value: "4.8★", Label: "Parent rating", SortOrder: 3},
			{Value: "24/7", Label: "Always available", SortOrder: 4},
		}
		return db.Create(&stats).Error
	}); err != nil {
		return false, err
	} else if n {
		seeded = true
	}

	// Features.
	if n, err := seedIfEmpty(db, &model.LandingFeature{}, func() error {
		features := []model.LandingFeature{
			{Icon: "book", Title: "Book-based answers", SortOrder: 1,
				Description: "Every explanation comes from your child's own textbook — accurate to the syllabus."},
			{Icon: "calendar", Title: "Study timeline", SortOrder: 2,
				Description: "Homework is split into daily tasks with reminders, so nothing is left to the last minute."},
			{Icon: "voice", Title: "Oral tests", SortOrder: 3,
				Description: "The AI asks questions aloud and checks spoken answers — great for revision."},
			{Icon: "edit", Title: "Written tests with marks", SortOrder: 4,
				Description: "Typed or photo answers are graded with marks and reasons for every mistake."},
			{Icon: "insights", Title: "Parent reports", SortOrder: 5,
				Description: "Weekly WhatsApp report and a live dashboard of scores, time and weak topics."},
			{Icon: "translate", Title: "Tamil & English medium", SortOrder: 6,
				Description: "Works for both Tamil-medium and English-medium students across boards."},
		}
		return db.Create(&features).Error
	}); err != nil {
		return false, err
	} else if n {
		seeded = true
	}

	// Testimonials.
	if n, err := seedIfEmpty(db, &model.LandingTestimonial{}, func() error {
		reviews := []model.LandingTestimonial{
			{Name: "Latha R", Sub: "Parent · Class 8 · Madurai", Rating: 5, SortOrder: 1,
				Quote: "My daughter finally studies on her own. The daily tasks and the WhatsApp report changed everything for us."},
			{Name: "Arjun M", Sub: "Class 10 student · Chennai", Rating: 5, SortOrder: 2,
				Quote: "It explains from my own Samacheer book, not random videos. My Science marks went from 62 to 84."},
			{Name: "Fathima S", Sub: "Parent · Class 6 · Trichy", Rating: 5, SortOrder: 3,
				Quote: "The Tamil-medium explanations are so clear. My son actually enjoys revising now."},
			{Name: "Karthik V", Sub: "Parent · Class 12 · Coimbatore", Rating: 4, SortOrder: 4,
				Quote: "The oral tests are brilliant for board prep. Wish it had more past-paper questions — but great value."},
			{Name: "Deepa N", Sub: "Parent · Class 9 · Salem", Rating: 5, SortOrder: 5,
				Quote: "I finally know his weak topics every week instead of guessing after tuition. Worth every rupee."},
			{Name: "Sanjay P", Sub: "Class 11 student · Madurai", Rating: 5, SortOrder: 6,
				Quote: "Uploaded a photo of my homework and got a full plan with tests in seconds. Super useful."},
			{Name: "Ramesh K", Sub: "Parent · Class 7 · Erode", Rating: 5, SortOrder: 7,
				Quote: "Cheaper than tuition and available 24/7. The mistake explanations build real understanding."},
			{Name: "Priya G", Sub: "Parent · Class 10 · Chennai", Rating: 5, SortOrder: 8,
				Quote: "The timeline keeps her on track without me nagging. The parent dashboard is my favourite part."},
			{Name: "Mohan R", Sub: "Parent · Class 8 · Tirunelveli", Rating: 4, SortOrder: 9,
				Quote: "Very helpful for daily homework. Would love a few more subjects, but Maths and Science are excellent."},
		}
		return db.Create(&reviews).Error
	}); err != nil {
		return false, err
	} else if n {
		seeded = true
	}

	// FAQs.
	if n, err := seedIfEmpty(db, &model.LandingFaq{}, func() error {
		faqs := []model.LandingFaq{
			{Question: "Which boards are supported?", SortOrder: 1,
				Answer: "State Board, CBSE and ICSE for classes 1–12. Content is matched to your child's board and class."},
			{Question: "Which languages does it support?", SortOrder: 2,
				Answer: "Both Tamil-medium and English-medium students. Explanations are in simple language from the textbook."},
			{Question: "How is this different from YouTube?", SortOrder: 3,
				Answer: "Answers come from your child's own book and syllabus — not random videos — and every homework is turned into a personal plan with tests and scores."},
			{Question: "Is my child's data safe?", SortOrder: 4,
				Answer: "Yes. We collect only what's needed to run the service and never sell personal data. Parents can request access or deletion anytime."},
			{Question: "What is the refund policy?", SortOrder: 5,
				Answer: "The 7-day trial is free with no card. Paid plans can be cancelled anytime and refunds follow our Refund Policy."},
			{Question: "Do I need to pay to start?", SortOrder: 6,
				Answer: "No — start with a 7-day free trial, no card required. Subscribe later if it helps your child."},
		}
		return db.Create(&faqs).Error
	}); err != nil {
		return false, err
	} else if n {
		seeded = true
	}

	// Text singleton.
	var textCount int64
	if err := db.Model(&model.LandingText{}).Count(&textCount).Error; err != nil {
		return false, err
	}
	if textCount == 0 {
		text := model.LandingText{
			ID:               1,
			HeroBadge:        "For Class 1–12 · Tamil & English medium",
			HeroTitle:        "Your child's personal AI tutor — answers from their own school books",
			HeroSubtitle:     "Upload homework as a photo, PDF or voice. Vaha AI explains every question from the textbook, plans it into daily tasks, then tests and scores your child — with reasons for every mistake.",
			HeroPrimaryCta:   "Start free — 7 days",
			HeroSecondaryCta: "Student login",
			HeroNote:         "No card required · Cancel anytime",
			FeaturesTitle:    "Everything a tuition teacher does — and it never tires",
			ReviewsTitle:     "Loved by parents & students",
			ReviewsRatingNote: "4.8 out of 5  ·  50+ reviews",
			FaqTitle:         "Common questions",
			ContactTitle:     "Get in touch",
			ContactSubtitle:  "Questions about plans, boards or your child's progress? We usually reply within a day.",
			ContactEmail:     "support@vahaai.com",
			ContactWhatsapp:  "+91 90000 00000",
			ContactHours:     "Mon–Sat · 9 AM – 7 PM IST",
			ContactOffice:    "Chennai, Tamil Nadu, India",
			CtaTitle:         "Give your child a personal AI tutor today",
			CtaSubtitle:      "Start with a free 7-day trial — no card required, cancel anytime.",
			CtaButton:        "Start free — 7 days",
			FooterDeveloper:    "KA Software",
			FooterDeveloperURL: "https://kasoftware.in/",
		}
		if err := db.Create(&text).Error; err != nil {
			return false, err
		}
		seeded = true
	}

	return seeded, nil
}

// seedIfEmpty runs create() only when the given model's table has no rows.
func seedIfEmpty(db *gorm.DB, m any, create func() error) (bool, error) {
	var count int64
	if err := db.Model(m).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	if err := create(); err != nil {
		return false, err
	}
	return true, nil
}
