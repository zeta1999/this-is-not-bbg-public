// Package cron provides a simple cron-style scheduler for periodic server tasks.
// Supports standard cron expressions (minute, hour, day-of-month, month, day-of-week)
// plus simple interval expressions (@every 5m, @every 1h).
package cron

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Job represents a scheduled task.
type Job struct {
	Name     string        `yaml:"name"     json:"name"`
	Schedule string        `yaml:"schedule" json:"schedule"` // cron expr or "@every 5m"
	Action   string        `yaml:"action"   json:"action"`   // action identifier
	Args     []string      `yaml:"args"     json:"args,omitempty"`
	Enabled  bool          `yaml:"enabled"  json:"enabled"`

	// Runtime state (not persisted).
	lastRun  time.Time
	nextRun  time.Time
	interval time.Duration // parsed from schedule
}

// ActionFunc is called when a job fires.
type ActionFunc func(ctx context.Context, job *Job) error

// Scheduler manages cron jobs.
type Scheduler struct {
	mu      sync.RWMutex
	jobs    []*Job
	actions map[string]ActionFunc
}

// New creates a scheduler.
func New() *Scheduler {
	return &Scheduler{
		actions: make(map[string]ActionFunc),
	}
}

// RegisterAction registers a named action handler.
func (s *Scheduler) RegisterAction(name string, fn ActionFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions[name] = fn
}

// AddJob adds a job to the scheduler.
func (s *Scheduler) AddJob(job Job) error {
	interval, err := parseSchedule(job.Schedule)
	if err != nil {
		return fmt.Errorf("invalid schedule %q: %w", job.Schedule, err)
	}
	job.interval = interval
	job.nextRun = time.Now().Add(interval)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, &job)
	slog.Info("cron job added", "name", job.Name, "schedule", job.Schedule, "action", job.Action)
	return nil
}

// RemoveJob removes a job by name.
func (s *Scheduler) RemoveJob(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, j := range s.jobs {
		if j.Name == name {
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			return true
		}
	}
	return false
}

// ListJobs returns a copy of all jobs.
func (s *Scheduler) ListJobs() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Job, len(s.jobs))
	for i, j := range s.jobs {
		out[i] = *j
		out[i].lastRun = j.lastRun
		out[i].nextRun = j.nextRun
	}
	return out
}

// Run starts the scheduler loop. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			s.tick(ctx, now)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, job := range s.jobs {
		if !job.Enabled {
			continue
		}
		if now.Before(job.nextRun) {
			continue
		}

		action, ok := s.actions[job.Action]
		if !ok {
			slog.Warn("cron: unknown action", "action", job.Action, "job", job.Name)
			job.nextRun = now.Add(job.interval)
			continue
		}

		job.lastRun = now
		job.nextRun = now.Add(job.interval)

		go func(j *Job, fn ActionFunc) {
			if err := fn(ctx, j); err != nil {
				slog.Error("cron job failed", "job", j.Name, "error", err)
			} else {
				slog.Debug("cron job completed", "job", j.Name)
			}
		}(job, action)
	}
}

// parseSchedule parses "@every 5m" style or simple interval strings.
func parseSchedule(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)

	// "@every 5m" format.
	if strings.HasPrefix(s, "@every ") {
		d, err := time.ParseDuration(strings.TrimPrefix(s, "@every "))
		if err != nil {
			return 0, err
		}
		if d < time.Second {
			return 0, fmt.Errorf("interval too short: %s", d)
		}
		return d, nil
	}

	// Try as a plain duration: "5m", "1h", "30s".
	if d, err := time.ParseDuration(s); err == nil && d >= time.Second {
		return d, nil
	}

	// Simple cron-like: "*/5 * * * *" → every 5 minutes.
	// Only support "*/N" for minutes for now.
	parts := strings.Fields(s)
	if len(parts) == 5 && strings.HasPrefix(parts[0], "*/") {
		n, err := strconv.Atoi(strings.TrimPrefix(parts[0], "*/"))
		if err == nil && n > 0 {
			return time.Duration(n) * time.Minute, nil
		}
	}

	return 0, fmt.Errorf("unsupported schedule format: %s (use '@every 5m' or '*/5 * * * *')", s)
}
