// Package scheduler runs periodic background jobs (currently: expiring trials
// whose window has passed without autopay). It's a plain time.Ticker goroutine —
// no external cron dependency — started at boot and stopped on shutdown.
package scheduler

import (
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/pkg/logger"
)

// Scheduler owns the background job loop.
type Scheduler struct {
	students *repository.StudentRepository
	log      *logger.Logger
	stop     chan struct{}
}

// New builds a Scheduler.
func New(students *repository.StudentRepository, log *logger.Logger) *Scheduler {
	return &Scheduler{students: students, log: log, stop: make(chan struct{})}
}

// Start runs the jobs once immediately, then on the given interval until Stop.
func (s *Scheduler) Start(interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		s.runOnce()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.runOnce()
			case <-s.stop:
				return
			}
		}
	}()
}

// Stop halts the loop.
func (s *Scheduler) Stop() { close(s.stop) }

// runOnce executes each periodic job, logging outcomes. A job failure is logged
// and skipped — it must never take the process down.
func (s *Scheduler) runOnce() {
	n, err := s.students.ExpireOverdueTrials(time.Now())
	if err != nil {
		s.log.Errorf("scheduler: expire trials: %v", err)
		return
	}
	if n > 0 {
		s.log.Infof("scheduler: expired %d trial(s) with no autopay", n)
	}
}
