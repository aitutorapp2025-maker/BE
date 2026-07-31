// Package scheduler runs admin-managed background jobs. Jobs are registered in
// code (with their dependencies) and enabled/disabled from the admin panel via
// the cron_jobs table — each tick the scheduler runs only the enabled jobs and
// records the outcome. It's a plain time.Ticker goroutine (no cron dependency).
package scheduler

import (
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// Job is a registered background task. Run returns a short human-readable result
// (e.g. "reminded 12") stored as the last result.
type Job struct {
	Key      string
	Schedule string // "hourly" (every tick) or "daily" (once per calendar day)
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

// runDue runs each enabled, due job. A job failure is recorded and logged but
// never takes the process down.
func (s *Scheduler) runDue(now time.Time) {
	for _, j := range s.jobs {
		cj, err := s.crons.FindByKey(j.Key)
		if err != nil || cj == nil || !cj.Enabled {
			continue
		}
		// Daily jobs run at most once per calendar day.
		if j.Schedule == "daily" && cj.LastRunAt != nil && sameDay(*cj.LastRunAt, now) {
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
