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

// csvResponse renders rows to CSV and returns the download envelope, including
// the number of data records (so the admin sees how many the filters matched).
func csvResponse(c *fiber.Ctx, name string, recordCount int, header []string, rows [][]string) error {
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
	return c.JSON(fiber.Map{
		"success":  true,
		"filename": filename,
		"rows":     recordCount,
		"content":  b.String(),
	})
}

// dateRange parses ?from & ?to (YYYY-MM-DD). `to` is made exclusive of the next
// day so the whole end date is included. Either may be nil (unbounded).
func dateRange(c *fiber.Ctx) (from, to *time.Time) {
	if v := strings.TrimSpace(c.Query("from")); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			from = &t
		}
	}
	if v := strings.TrimSpace(c.Query("to")); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.AddDate(0, 0, 1)
			to = &end
		}
	}
	return
}

// within reports whether t is inside [from, to) (nil bounds are open).
func within(t time.Time, from, to *time.Time) bool {
	if from != nil && t.Before(*from) {
		return false
	}
	if to != nil && !t.Before(*to) {
		return false
	}
	return true
}

// eqFilter reports whether the ?key query is empty/"all" (match everything) or
// equals value (case-insensitive).
func eqFilter(c *fiber.Ctx, key, value string) bool {
	q := strings.TrimSpace(c.Query(key))
	if q == "" || strings.EqualFold(q, "all") {
		return true
	}
	return strings.EqualFold(q, value)
}

func rupees(paise int64) string { return fmt.Sprintf("%.2f", float64(paise)/100) }

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// Students exports students, filtered by join-date range and optional
// board / medium / pay_status / plan / class query params.
// GET /api/v1/admin/reports/students?from=&to=&board=&medium=&pay_status=&plan=&class=
func (h *ReportHandler) Students(c *fiber.Ctx) error {
	students, err := h.students.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load students")
	}
	from, to := dateRange(c)
	header := []string{"ID", "Name", "Phone", "Parent Phone", "Class", "Group",
		"Board", "Medium", "Language", "Plan", "Pay Status", "Credits",
		"Autopay", "Trial Ends", "Joined"}
	rows := make([][]string, 0, len(students))
	for _, s := range students {
		if !within(s.JoinedAt, from, to) ||
			!eqFilter(c, "board", s.Board) ||
			!eqFilter(c, "medium", s.Medium) ||
			!eqFilter(c, "pay_status", s.PayStatus) ||
			!eqFilter(c, "plan", s.Plan) ||
			!eqFilter(c, "class", s.StudentClass) {
			continue
		}
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
	return csvResponse(c, "students-report", len(rows), header, rows)
}

// Payments exports money-in events (plan grants + recharges), filtered by date
// range and optional ?type=grant|recharge.
// GET /api/v1/admin/reports/payments?from=&to=&type=
func (h *ReportHandler) Payments(c *fiber.Ctx) error {
	from, to := dateRange(c)
	kind := strings.TrimSpace(c.Query("type"))
	if strings.EqualFold(kind, "all") {
		kind = ""
	}
	entries, err := h.credits.RevenueEntriesFiltered(from, to, kind)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load payments")
	}
	// Resolve student names once (id→name projection, not full rows).
	names, err := h.students.NamesByID()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load students")
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
	count := len(rows)
	// Totals row.
	rows = append(rows, []string{"", "", "", "", "TOTAL", rupees(total), ""})
	return csvResponse(c, "payments-report", count, header, rows)
}

// Summary exports the profit & loss overview for an optional date range.
// GET /api/v1/admin/reports/summary?from=&to=
func (h *ReportHandler) Summary(c *fiber.Ctx) error {
	from, to := dateRange(c)
	p, err := h.credits.SummaryRange(from, to)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load summary")
	}
	totalStudents, err := h.students.Count()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load students")
	}
	profit := p.RevenuePaise - p.AICostPaise
	margin := "0.0"
	if p.RevenuePaise > 0 {
		margin = fmt.Sprintf("%.1f", float64(profit)/float64(p.RevenuePaise)*100)
	}
	period := "All time"
	if from != nil || to != nil {
		lo, hi := "…", "…"
		if from != nil {
			lo = ts(*from)
		}
		if to != nil {
			hi = ts(to.AddDate(0, 0, -1)) // show the inclusive end date
		}
		period = lo + " to " + hi
	}
	header := []string{"Metric", "Value"}
	rows := [][]string{
		{"Period", period},
		{"Total revenue (₹)", rupees(p.RevenuePaise)},
		{"Total AI cost (₹)", rupees(p.AICostPaise)},
		{"Gross profit (₹)", rupees(profit)},
		{"Gross margin (%)", margin},
		{"Paying students", fmt.Sprint(p.Students)},
		{"Total students (all)", fmt.Sprint(totalStudents)},
		{"AI actions (debits)", fmt.Sprint(p.Debits)},
		{"Generated", time.Now().Format("2006-01-02 15:04")},
	}
	return csvResponse(c, "summary-report", len(rows), header, rows)
}

func boolYesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}
