package handler

import (
	"encoding/csv"
	"fmt"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// ReportHandler builds CSV exports (Excel-openable) for the admin panel. Each
// endpoint returns {"filename","content"} JSON — the content is the CSV text,
// which the admin app saves as a download. Responses are E2E-encrypted like the
// rest of the admin API.
type ReportHandler struct {
	students *repository.StudentRepository
	credits  *repository.CreditRepository
}

// NewReportHandler builds a ReportHandler.
func NewReportHandler(students *repository.StudentRepository, credits *repository.CreditRepository) *ReportHandler {
	return &ReportHandler{students: students, credits: credits}
}

// csvResponse renders rows to CSV and returns the download envelope.
func csvResponse(c *fiber.Ctx, name string, header []string, rows [][]string) error {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write(header)
	for _, r := range rows {
		_ = w.Write(r)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to build report")
	}
	filename := fmt.Sprintf("%s-%s.csv", name, time.Now().Format("2006-01-02"))
	return c.JSON(fiber.Map{"success": true, "filename": filename, "content": b.String()})
}

func rupees(paise int64) string { return fmt.Sprintf("%.2f", float64(paise)/100) }

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// Students exports every student. GET /api/v1/admin/reports/students
func (h *ReportHandler) Students(c *fiber.Ctx) error {
	students, err := h.students.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load students")
	}
	header := []string{"ID", "Name", "Phone", "Parent Phone", "Class", "Group",
		"Board", "Medium", "Language", "Plan", "Pay Status", "Credits",
		"Autopay", "Trial Ends", "Joined"}
	rows := make([][]string, 0, len(students))
	for _, s := range students {
		trial := ""
		if s.TrialEndsAt != nil {
			trial = ts(*s.TrialEndsAt)
		}
		rows = append(rows, []string{
			fmt.Sprint(s.ID), s.Name, s.Phone, s.ParentPhone, s.StudentClass,
			s.StudentGroup, s.Board, s.Medium, s.TeachingLanguage, s.Plan,
			s.PayStatus, fmt.Sprint(s.Credits), boolYesNo(s.AutopayActive),
			trial, ts(s.JoinedAt),
		})
	}
	return csvResponse(c, "students-report", header, rows)
}

// Payments exports every money-in event (plan grants + recharges).
// GET /api/v1/admin/reports/payments
func (h *ReportHandler) Payments(c *fiber.Ctx) error {
	entries, err := h.credits.RevenueEntries()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load payments")
	}
	// Resolve student names once.
	students, err := h.students.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load students")
	}
	names := make(map[uint]string, len(students))
	for _, s := range students {
		names[s.ID] = s.Name
	}

	header := []string{"Date", "Student ID", "Student", "Type", "Credits",
		"Amount (₹)", "Note"}
	rows := make([][]string, 0, len(entries))
	var total int64
	for _, e := range entries {
		total += e.RevenuePaise
		rows = append(rows, []string{
			ts(e.CreatedAt), fmt.Sprint(e.StudentID), names[e.StudentID],
			e.Kind, fmt.Sprint(e.Credits), rupees(e.RevenuePaise), e.Note,
		})
	}
	// Totals row.
	rows = append(rows, []string{"", "", "", "", "TOTAL", rupees(total), ""})
	return csvResponse(c, "payments-report", header, rows)
}

// Summary exports the profit & loss overview. GET /api/v1/admin/reports/summary
func (h *ReportHandler) Summary(c *fiber.Ctx) error {
	p, err := h.credits.Summary()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load summary")
	}
	students, err := h.students.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load students")
	}
	profit := p.RevenuePaise - p.AICostPaise
	margin := "0.0"
	if p.RevenuePaise > 0 {
		margin = fmt.Sprintf("%.1f", float64(profit)/float64(p.RevenuePaise)*100)
	}
	header := []string{"Metric", "Value"}
	rows := [][]string{
		{"Total revenue (₹)", rupees(p.RevenuePaise)},
		{"Total AI cost (₹)", rupees(p.AICostPaise)},
		{"Gross profit (₹)", rupees(profit)},
		{"Gross margin (%)", margin},
		{"Paying students", fmt.Sprint(p.Students)},
		{"Total students", fmt.Sprint(len(students))},
		{"AI actions (debits)", fmt.Sprint(p.Debits)},
		{"Generated", time.Now().Format("2006-01-02 15:04")},
	}
	return csvResponse(c, "summary-report", header, rows)
}

func boolYesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}
