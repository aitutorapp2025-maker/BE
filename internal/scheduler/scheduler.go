// Package scheduler runs admin-managed background jobs. Jobs are registered in
// code (with their dependencies) and enabled/disabled from the admin panel via
// the cron_jobs table — each tick the scheduler runs only the enabled jobs and
// records the outcome. It's a plain time.Ticker goroutine (no cron dependency).
package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// Job is a registered background task. Run returns a short human-readable result
// (e.g. "reminded 12") stored as the last result.
type Job struct {
	Key      string
	Schedule string // "minutely" (every tick), "hourly", "daily", "daily@H" (once/day, from hour H), "everyNdays"
	Run      func(now time.Time) (string, error)
}

// Scheduler owns the background job loop.
type Scheduler struct {
	crons *repository.CronRepository
	jobs  []Job
	log   *logger.Logger
	stop  chan struct{}
}

// New builds a Scheduler backed by the cron_jobs table.
func New(crons *repository.CronRepository, log *logger.Logger) *Scheduler {
	return &Scheduler{crons: crons, log: log, stop: make(chan struct{})}
}

// Register adds a job to the registry (call before Start).
func (s *Scheduler) Register(j Job) { s.jobs = append(s.jobs, j) }

// Start runs due jobs once immediately, then on the given interval until Stop.
func (s *Scheduler) Start(interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		s.runDue(time.Now())
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.runDue(time.Now())
			case <-s.stop:
				return
			}
		}
	}()
}

// Stop halts the loop.
func (s *Scheduler) Stop() { close(s.stop) }

// RunNow runs a registered job immediately, ignoring the enabled/daily gates
// (for admin "Run now"). It records the outcome and returns the result string
// and whether the job key was found.
func (s *Scheduler) RunNow(key string) (result string, found bool) {
	for _, j := range s.jobs {
		if j.Key != key {
			continue
		}
		now := time.Now()
		r, rerr := j.Run(now)
		status := "ok"
		if rerr != nil {
			status = "error"
			r = trunc(rerr.Error(), 200)
			s.log.Errorf("cron %s (run now): %v", key, rerr)
		} else {
			s.log.Infof("cron %s (run now): %s", key, r)
		}
		if err := s.crons.RecordRun(key, status, r, now); err != nil {
			s.log.Errorf("cron %s: record run: %v", key, err)
		}
		return r, true
	}
	return "", false
}

// runDue runs each enabled, due job. A job failure is recorded and logged but
// never takes the process down.
func (s *Scheduler) runDue(now time.Time) {
	for _, j := range s.jobs {
		cj, err := s.crons.FindByKey(j.Key)
		if err != nil || cj == nil || !cj.Enabled {
			continue
		}
		// The ADMIN's schedule (cron panel) wins; the code schedule is only the
		// default the row was seeded with.
		schedule := strings.TrimSpace(cj.Schedule)
		if schedule == "" {
			schedule = j.Schedule
		}
		if !scheduleDue(schedule, cj.LastRunAt, now) {
			continue
		}
		result, rerr := j.Run(now)
		status := "ok"
		if rerr != nil {
			status = "error"
			result = trunc(rerr.Error(), 200)
			s.log.Errorf("cron %s: %v", j.Key, rerr)
		} else {
			s.log.Infof("cron %s: %s", j.Key, result)
		}
		if err := s.crons.RecordRun(j.Key, status, result, now); err != nil {
			s.log.Errorf("cron %s: record run: %v", j.Key, err)
		}
	}
}

// scheduleDue reports whether a job with the given schedule should run now.
// Supports the legacy tokens (minutely / hourly / daily / daily@H /
// everyNdays) and standard 5-field cron expressions ("m h dom mon dow").
func scheduleDue(schedule string, lastRun *time.Time, now time.Time) bool {
	switch {
	case schedule == "minutely" || schedule == "":
		return true
	case schedule == "hourly":
		return lastRun == nil || now.Sub(*lastRun) >= time.Hour
	case schedule == "daily":
		return lastRun == nil || !sameDay(*lastRun, now)
	}
	if h, ok := dailyAtHour(schedule); ok {
		return now.Hour() >= h && (lastRun == nil || !sameDay(*lastRun, now))
	}
	if days, ok := everyNDays(schedule); ok {
		return lastRun == nil || now.Sub(*lastRun) >= time.Duration(days)*24*time.Hour
	}
	if expr, ok := parseCron(schedule); ok {
		// The expression matches specific minutes; never double-run within the
		// same minute (the tick is minutely, but Start also runs at boot).
		if lastRun != nil && lastRun.Truncate(time.Minute).Equal(now.Truncate(time.Minute)) {
			return false
		}
		return expr.matches(now)
	}
	return false // unknown schedule — safer to not run than to spam
}

// ValidSchedule reports whether an admin-entered schedule string is usable.
func ValidSchedule(schedule string) bool {
	s := strings.TrimSpace(schedule)
	if s == "minutely" || s == "hourly" || s == "daily" {
		return true
	}
	if _, ok := dailyAtHour(s); ok {
		return true
	}
	if _, ok := everyNDays(s); ok {
		return true
	}
	_, ok := parseCron(s)
	return ok
}

// cronExpr is a parsed 5-field cron expression: minute hour day-of-month
// month day-of-week. Supports "*", numbers, comma lists, ranges "a-b" and
// steps "*/n" or "a-b/n".
type cronExpr struct {
	minute, hour, dom, mon, dow map[int]bool
}

func (e cronExpr) matches(t time.Time) bool {
	return e.minute[t.Minute()] && e.hour[t.Hour()] && e.dom[t.Day()] &&
		e.mon[int(t.Month())] && e.dow[int(t.Weekday())]
}

func parseCron(s string) (cronExpr, bool) {
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return cronExpr{}, false
	}
	bounds := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	sets := make([]map[int]bool, 5)
	for i, f := range fields {
		set, ok := parseCronField(f, bounds[i][0], bounds[i][1])
		if !ok {
			return cronExpr{}, false
		}
		sets[i] = set
	}
	return cronExpr{
		minute: sets[0], hour: sets[1], dom: sets[2], mon: sets[3], dow: sets[4],
	}, true
}

func parseCronField(f string, lo, hi int) (map[int]bool, bool) {
	out := map[int]bool{}
	for _, part := range strings.Split(f, ",") {
		step := 1
		if i := strings.IndexByte(part, '/'); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return nil, false
			}
			step = n
			part = part[:i]
		}
		from, to := lo, hi
		switch {
		case part == "*" || part == "":
			// full range
		case strings.Contains(part, "-"):
			bits := strings.SplitN(part, "-", 2)
			a, err1 := strconv.Atoi(bits[0])
			b, err2 := strconv.Atoi(bits[1])
			if err1 != nil || err2 != nil || a < lo || b > hi || a > b {
				return nil, false
			}
			from, to = a, b
		default:
			n, err := strconv.Atoi(part)
			if err != nil || n < lo || n > hi {
				return nil, false
			}
			from, to = n, n
		}
		for v := from; v <= to; v += step {
			out[v] = true
		}
	}
	return out, len(out) > 0
}

// everyNDays parses an "everyNdays" schedule (e.g. "every3days") into N. Returns
// (0, false) for any other schedule string.
func everyNDays(schedule string) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(schedule, "every%ddays", &n); err == nil && n > 0 {
		return n, true
	}
	return 0, false
}

// dailyAtHour parses a "daily@H" schedule (e.g. "daily@19") into H. Returns
// (0, false) for any other schedule string.
func dailyAtHour(schedule string) (int, bool) {
	var h int
	if _, err := fmt.Sscanf(schedule, "daily@%d", &h); err == nil && h >= 0 && h <= 23 {
		return h, true
	}
	return 0, false
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
