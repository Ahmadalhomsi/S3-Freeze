// Package scheduler triggers backup runs from job cron schedules.
package scheduler

import (
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"s3freeze/internal/engine"
	"s3freeze/internal/store"
)

type Scheduler struct {
	cron  *cron.Cron
	store *store.Store
	eng   *engine.Engine

	mu      sync.Mutex
	entries map[int64]cron.EntryID
}

func New(st *store.Store, eng *engine.Engine) *Scheduler {
	return &Scheduler{
		cron:    cron.New(),
		store:   st,
		eng:     eng,
		entries: map[int64]cron.EntryID{},
	}
}

func (s *Scheduler) Start() { s.cron.Start() }

func (s *Scheduler) Stop() { <-s.cron.Stop().Done() }

// Parse accepts standard 5-field cron expressions and descriptors such as
// "@daily" or "@every 6h". An empty spec means manual-only.
func Parse(spec string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("empty schedule")
	}
	return cron.ParseStandard(spec)
}

// Interval estimates the time between two consecutive scheduled runs.
func Interval(spec string) time.Duration {
	sched, err := Parse(spec)
	if err != nil {
		return 0
	}
	t1 := sched.Next(time.Now())
	return sched.Next(t1).Sub(t1)
}

// Reload replaces all scheduled entries with the current enabled jobs.
func (s *Scheduler) Reload() error {
	jobs, err := s.store.ListJobs()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, entry := range s.entries {
		s.cron.Remove(entry)
		delete(s.entries, id)
	}
	for _, j := range jobs {
		if !j.Enabled || strings.TrimSpace(j.Schedule) == "" {
			continue
		}
		sched, err := Parse(j.Schedule)
		if err != nil {
			slog.Warn("invalid schedule", "job", j.Name, "schedule", j.Schedule, "err", err)
			continue
		}
		jobID, name := j.ID, j.Name
		s.entries[jobID] = s.cron.Schedule(sched, cron.FuncJob(func() {
			if _, err := s.eng.StartBackup(jobID); err != nil {
				slog.Warn("scheduled backup not started", "job", name, "err", err)
			}
		}))
	}
	return nil
}

// Next returns the next scheduled run time of a job, if any.
func (s *Scheduler) Next(jobID int64) *time.Time {
	s.mu.Lock()
	id, ok := s.entries[jobID]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	next := s.cron.Entry(id).Next
	if next.IsZero() {
		// The cron loop computes Next lazily after Start; fall back to the schedule.
		e := s.cron.Entry(id)
		if e.Schedule == nil {
			return nil
		}
		next = e.Schedule.Next(time.Now())
	}
	return &next
}
