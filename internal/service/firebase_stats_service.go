package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/bigquery"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
)

// QueueFirebaseSync is the RabbitMQ queue for background BigQuery-sync jobs, so
// the admin "Sync now" click returns immediately and the (potentially slow)
// BigQuery queries run in a worker.
const QueueFirebaseSync = "firebase.stats.sync"

// SyncJob asks the worker to sync the last Days days.
type SyncJob struct {
	Days int `json:"days"`
}

// FirebaseStatsService syncs the Firebase Analytics + Crashlytics BigQuery
// exports into our own daily-aggregate tables (so the admin dashboards read from
// our DB). It authenticates to BigQuery with the FCM service-account JSON.
type FirebaseStatsService struct {
	settings *repository.SettingRepository
	stats    *repository.FirebaseStatsRepository
}

// NewFirebaseStatsService builds a FirebaseStatsService.
func NewFirebaseStatsService(settings *repository.SettingRepository, stats *repository.FirebaseStatsRepository) *FirebaseStatsService {
	return &FirebaseStatsService{settings: settings, stats: stats}
}

type nameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type issueCount struct {
	IssueID string `json:"issue_id"`
	Title   string `json:"title"`
	Count   int    `json:"count"`
}

// Sync pulls the last `days` calendar days (ending yesterday — the export lands
// with a delay) for whichever of Analytics/Crashlytics is enabled + configured,
// and upserts the daily aggregates. Missing tables (no export yet) are skipped,
// not treated as errors.
func (s *FirebaseStatsService) Sync(ctx context.Context, days int) (string, error) {
	set, err := s.settings.Get()
	if err != nil {
		return "", err
	}
	if !set.AnalyticsEnabled && !set.CrashlyticsEnabled {
		return "analytics & crashlytics off — skipped", nil
	}
	sa := strings.TrimSpace(set.FcmServiceAccount)
	if sa == "" {
		return "no Firebase service account configured — skipped", nil
	}
	bq, err := bigquery.New(sa, set.BigQueryProjectID)
	if err != nil {
		return "", err
	}
	if days <= 0 {
		days = 3
	}
	now := time.Now().UTC()
	aDone, cDone := 0, 0
	aMissing, cMissing := false, false
	var firstErr error
	for i := 1; i <= days; i++ {
		d := now.AddDate(0, 0, -i)
		if set.AnalyticsEnabled && strings.TrimSpace(set.AnalyticsDataset) != "" {
			switch err := s.syncAnalyticsDay(ctx, bq, set, d); {
			case err == nil:
				aDone++
			case errors.Is(err, errMissingTable):
				aMissing = true
			default:
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		if set.CrashlyticsEnabled && strings.TrimSpace(set.CrashlyticsTable) != "" {
			switch err := s.syncCrashDay(ctx, bq, set, d); {
			case err == nil:
				cDone++
			case errors.Is(err, errMissingTable):
				cMissing = true
			default:
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	// Build a diagnostic summary so a 0 result explains itself.
	var parts []string
	if set.AnalyticsEnabled {
		switch {
		case strings.TrimSpace(set.AnalyticsDataset) == "":
			parts = append(parts, "analytics: dataset not set in Settings")
		case aMissing && aDone == 0:
			parts = append(parts, "analytics: no export table in BigQuery yet (link GA4 → BigQuery, then wait for the daily export)")
		default:
			parts = append(parts, fmt.Sprintf("analytics=%d day(s)", aDone))
		}
	}
	if set.CrashlyticsEnabled {
		switch {
		case strings.TrimSpace(set.CrashlyticsTable) == "":
			parts = append(parts, "crashlytics: table name not set in Settings")
		case cMissing && cDone == 0:
			parts = append(parts, "crashlytics: export table not found in BigQuery — enable Crashlytics → BigQuery export (Blaze plan) and wait for the daily export")
		default:
			parts = append(parts, fmt.Sprintf("crashlytics=%d day(s)", cDone))
		}
	}
	summary := "synced — " + strings.Join(parts, "; ")
	if firstErr != nil {
		summary += " — error: " + firstErr.Error()
	}
	// Persist so the admin dashboard can show why a sync produced 0.
	_ = s.settings.SetSyncStatus(summary)
	return summary, firstErr
}

func (s *FirebaseStatsService) syncAnalyticsDay(ctx context.Context, bq *bigquery.Client, set *model.Setting, d time.Time) error {
	dayStr := d.Format("2006-01-02")
	ymd := d.Format("20060102")
	tbl := fmt.Sprintf("`%s.%s.events_%s`", bq.ProjectID(), set.AnalyticsDataset, ymd)

	summary, err := bq.Query(ctx, fmt.Sprintf(`SELECT
  COUNT(DISTINCT user_pseudo_id) AS active_users,
  COUNT(DISTINCT IF(event_name='first_open', user_pseudo_id, NULL)) AS new_users,
  COUNT(DISTINCT CONCAT(user_pseudo_id, CAST((SELECT value.int_value FROM UNNEST(event_params) WHERE key='ga_session_id') AS STRING))) AS sessions,
  COUNT(*) AS event_count
FROM %s`, tbl))
	if err != nil {
		return skipMissing(err)
	}

	row := &model.AnalyticsDaily{Day: dayStr, UpdatedAt: time.Now()}
	if len(summary) > 0 {
		row.ActiveUsers = atoi(summary[0]["active_users"])
		row.NewUsers = atoi(summary[0]["new_users"])
		row.Sessions = atoi(summary[0]["sessions"])
		row.EventCount = atoi(summary[0]["event_count"])
	}

	if hourly, err := bq.Query(ctx, fmt.Sprintf(
		`SELECT EXTRACT(HOUR FROM TIMESTAMP_MICROS(event_timestamp)) AS h, COUNT(*) AS c FROM %s GROUP BY h`, tbl)); err == nil {
		row.HourlyEvents = hourlyJSON(hourly)
	}
	if top, err := bq.Query(ctx, fmt.Sprintf(
		`SELECT event_name AS name, COUNT(*) AS c FROM %s GROUP BY name ORDER BY c DESC LIMIT 10`, tbl)); err == nil {
		row.TopEvents = nameCountJSON(top)
	}
	if plat, err := bq.Query(ctx, fmt.Sprintf(
		`SELECT platform AS name, COUNT(*) AS c FROM %s GROUP BY name ORDER BY c DESC`, tbl)); err == nil {
		row.Platforms = nameCountJSON(plat)
	}
	return s.stats.UpsertAnalytics(row)
}

func (s *FirebaseStatsService) syncCrashDay(ctx context.Context, bq *bigquery.Client, set *model.Setting, d time.Time) error {
	dayStr := d.Format("2006-01-02")
	tbl := fmt.Sprintf("`%s.%s.%s`", bq.ProjectID(), crashDataset(set), set.CrashlyticsTable)
	where := fmt.Sprintf("WHERE DATE(event_timestamp) = '%s' AND is_fatal", dayStr)

	summary, err := bq.Query(ctx, fmt.Sprintf(
		`SELECT COUNT(*) AS crashes, COUNT(DISTINCT installation_uuid) AS affected FROM %s %s`, tbl, where))
	if err != nil {
		return skipMissing(err)
	}

	row := &model.CrashDaily{Day: dayStr, UpdatedAt: time.Now()}
	if len(summary) > 0 {
		row.Crashes = atoi(summary[0]["crashes"])
		row.AffectedUsers = atoi(summary[0]["affected"])
	}
	// Crash-free % from that day's active users (analytics synced first).
	if a, err := s.stats.AnalyticsForDay(dayStr); err == nil && a.ActiveUsers > 0 {
		free := 100.0 * float64(a.ActiveUsers-row.AffectedUsers) / float64(a.ActiveUsers)
		if free < 0 {
			free = 0
		}
		if free > 100 {
			free = 100
		}
		row.CrashFreeUsers = float64(int(free*100)) / 100 // 2 dp
	}

	if hourly, err := bq.Query(ctx, fmt.Sprintf(
		`SELECT EXTRACT(HOUR FROM event_timestamp) AS h, COUNT(*) AS c FROM %s %s GROUP BY h`, tbl, where)); err == nil {
		row.HourlyCrashes = hourlyJSON(hourly)
	}
	if top, err := bq.Query(ctx, fmt.Sprintf(
		`SELECT issue_id, ANY_VALUE(issue_title) AS title, COUNT(*) AS c FROM %s %s GROUP BY issue_id ORDER BY c DESC LIMIT 10`, tbl, where)); err == nil {
		row.TopIssues = issueJSON(top)
	}
	if ver, err := bq.Query(ctx, fmt.Sprintf(
		`SELECT application.display_version AS name, COUNT(*) AS c FROM %s %s GROUP BY name ORDER BY c DESC LIMIT 10`, tbl, where)); err == nil {
		row.Versions = nameCountJSON(ver)
	}
	return s.stats.UpsertCrash(row)
}

func crashDataset(set *model.Setting) string {
	if v := strings.TrimSpace(set.CrashlyticsDataset); v != "" {
		return v
	}
	return "firebase_crashlytics"
}

// errMissingTable marks a "BigQuery table not found" (the export doesn't exist
// yet). It's not a hard failure — the sync reports it so a 0 result is
// explained rather than looking like a silent success.
var errMissingTable = errors.New("bigquery export table not found")

// skipMissing maps a "table not found" to errMissingTable; other errors pass
// through unchanged.
func skipMissing(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "not found") {
		return errMissingTable
	}
	return err
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func hourlyJSON(rows []map[string]string) string {
	var h [24]int
	for _, r := range rows {
		idx := atoi(r["h"])
		if idx >= 0 && idx < 24 {
			h[idx] = atoi(r["c"])
		}
	}
	b, _ := json.Marshal(h)
	return string(b)
}

func nameCountJSON(rows []map[string]string) string {
	out := make([]nameCount, 0, len(rows))
	for _, r := range rows {
		name := r["name"]
		if name == "" {
			name = "(unknown)"
		}
		out = append(out, nameCount{Name: name, Count: atoi(r["c"])})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func issueJSON(rows []map[string]string) string {
	out := make([]issueCount, 0, len(rows))
	for _, r := range rows {
		title := r["title"]
		if title == "" {
			title = "(unknown issue)"
		}
		out = append(out, issueCount{IssueID: r["issue_id"], Title: title, Count: atoi(r["c"])})
	}
	b, _ := json.Marshal(out)
	return string(b)
}
