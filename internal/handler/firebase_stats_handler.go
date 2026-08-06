package handler

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// FirebaseStatsHandler serves the admin Analytics + Crashlytics dashboards from
// our stored daily aggregates, and can trigger an on-demand sync.
type FirebaseStatsHandler struct {
	stats    *repository.FirebaseStatsRepository
	settings *repository.SettingRepository
	sync     *service.FirebaseStatsService
}

// NewFirebaseStatsHandler builds a FirebaseStatsHandler.
func NewFirebaseStatsHandler(stats *repository.FirebaseStatsRepository, settings *repository.SettingRepository, sync *service.FirebaseStatsService) *FirebaseStatsHandler {
	return &FirebaseStatsHandler{stats: stats, settings: settings, sync: sync}
}

func (h *FirebaseStatsHandler) days(c *fiber.Ctx) int {
	n, _ := strconv.Atoi(c.Query("days"))
	if n <= 0 || n > 180 {
		n = 30
	}
	return n
}

// Analytics returns the analytics dashboard payload. GET /admin/analytics?days=30
func (h *FirebaseStatsHandler) Analytics(c *fiber.Ctx) error {
	set, _ := h.settings.Get()
	rows, err := h.stats.ListAnalytics(h.days(c))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load analytics")
	}

	hourly := [24]int{}
	events := map[string]int{}
	platforms := map[string]int{}
	var totalEvents, totalNew, totalSessions, latestActive, peakActive int
	series := make([]fiber.Map, 0, len(rows))
	for _, r := range rows {
		totalEvents += r.EventCount
		totalNew += r.NewUsers
		totalSessions += r.Sessions
		latestActive = r.ActiveUsers // rows are oldest→newest
		if r.ActiveUsers > peakActive {
			peakActive = r.ActiveUsers
		}
		addHourly(&hourly, r.HourlyEvents)
		mergeCounts(events, r.TopEvents)
		mergeCounts(platforms, r.Platforms)
		series = append(series, fiber.Map{
			"day":          r.Day,
			"active_users": r.ActiveUsers,
			"new_users":    r.NewUsers,
			"sessions":     r.Sessions,
			"event_count":  r.EventCount,
		})
	}

	return c.JSON(fiber.Map{
		"success":    true,
		"enabled":    set != nil && set.AnalyticsEnabled,
		"configured": analyticsConfigured(set),
		"summary": fiber.Map{
			"active_users": latestActive,
			"peak_active":  peakActive,
			"new_users":    totalNew,
			"sessions":     totalSessions,
			"events":       totalEvents,
		},
		"hourly":     hourly,
		"top_events": topCounts(events, 10),
		"platforms":  topCounts(platforms, 10),
		"series":     series,
	})
}

// Crashlytics returns the crash dashboard payload. GET /admin/crashlytics?days=30
func (h *FirebaseStatsHandler) Crashlytics(c *fiber.Ctx) error {
	set, _ := h.settings.Get()
	rows, err := h.stats.ListCrash(h.days(c))
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not load crashlytics")
	}

	hourly := [24]int{}
	issues := map[string]issueAgg{}
	versions := map[string]int{}
	var totalCrashes, totalAffected, peakCrashes int
	var latestCrashFree float64
	series := make([]fiber.Map, 0, len(rows))
	for _, r := range rows {
		totalCrashes += r.Crashes
		totalAffected += r.AffectedUsers
		latestCrashFree = r.CrashFreeUsers
		if r.Crashes > peakCrashes {
			peakCrashes = r.Crashes
		}
		addHourly(&hourly, r.HourlyCrashes)
		mergeIssues(issues, r.TopIssues)
		mergeCounts(versions, r.Versions)
		series = append(series, fiber.Map{
			"day":              r.Day,
			"crashes":          r.Crashes,
			"affected_users":   r.AffectedUsers,
			"crash_free_users": r.CrashFreeUsers,
		})
	}

	return c.JSON(fiber.Map{
		"success":    true,
		"enabled":    set != nil && set.CrashlyticsEnabled,
		"configured": crashConfigured(set),
		"summary": fiber.Map{
			"crashes":          totalCrashes,
			"peak_crashes":     peakCrashes,
			"affected_users":   totalAffected,
			"crash_free_users": latestCrashFree,
		},
		"hourly":     hourly,
		"top_issues": topIssues(issues, 10),
		"versions":   topCounts(versions, 10),
		"series":     series,
	})
}

// Sync runs an on-demand BigQuery sync (last 7 days). POST /admin/firebase-stats/sync
func (h *FirebaseStatsHandler) Sync(c *fiber.Ctx) error {
	msg, err := h.sync.Sync(context.Background(), 7)
	if err != nil {
		return fiber.NewError(fiber.StatusBadGateway, "sync failed: "+err.Error())
	}
	return c.JSON(fiber.Map{"success": true, "message": msg})
}

func analyticsConfigured(s *model.Setting) bool {
	return s != nil && strings.TrimSpace(s.FcmServiceAccount) != "" &&
		strings.TrimSpace(s.AnalyticsDataset) != ""
}

func crashConfigured(s *model.Setting) bool {
	return s != nil && strings.TrimSpace(s.FcmServiceAccount) != "" &&
		strings.TrimSpace(s.CrashlyticsTable) != ""
}

// --- aggregation helpers ----------------------------------------------------

func addHourly(dst *[24]int, blob string) {
	var h [24]int
	if err := json.Unmarshal([]byte(blob), &h); err != nil {
		return
	}
	for i := 0; i < 24; i++ {
		dst[i] += h[i]
	}
}

func mergeCounts(dst map[string]int, blob string) {
	var arr []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(blob), &arr); err != nil {
		return
	}
	for _, e := range arr {
		dst[e.Name] += e.Count
	}
}

type issueAgg struct {
	IssueID string
	Count   int
}

func mergeIssues(dst map[string]issueAgg, blob string) {
	var arr []struct {
		IssueID string `json:"issue_id"`
		Title   string `json:"title"`
		Count   int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(blob), &arr); err != nil {
		return
	}
	for _, e := range arr {
		key := e.Title
		cur := dst[key]
		cur.IssueID = e.IssueID
		cur.Count += e.Count
		dst[key] = cur
	}
}

func topCounts(m map[string]int, n int) []fiber.Map {
	out := make([]fiber.Map, 0, len(m))
	for k, v := range m {
		out = append(out, fiber.Map{"name": k, "count": v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["count"].(int) > out[j]["count"].(int) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func topIssues(m map[string]issueAgg, n int) []fiber.Map {
	out := make([]fiber.Map, 0, len(m))
	for title, v := range m {
		out = append(out, fiber.Map{"title": title, "issue_id": v.IssueID, "count": v.Count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["count"].(int) > out[j]["count"].(int) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
