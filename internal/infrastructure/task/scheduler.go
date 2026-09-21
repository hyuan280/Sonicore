package task

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/logger"
)

const (
	StatusIdle     = "idle"
	StatusRunning  = "running"
	StatusDisabled = "disabled"
)

var (
	ErrNotFound       = errors.New("task not found")
	ErrAlreadyRunning = errors.New("task already running")
	ErrStopped        = errors.New("scheduler is stopped")
)

// Spec describes a scheduled task. Components register (or update) their
// periodic work by calling Scheduler.Register; the scheduler then owns the
// timing, execution and status reporting for every registered task.
//
// Exactly one of Interval or Cron must be set: Interval is a fixed period
// anchored to the registration/last run ("every 24h"), while Cron is a
// standard 5-field cron expression anchored to wall-clock time ("0 3 * * *",
// every day at 03:00). Cron expressions accept the usual ranges/steps/lists,
// descriptors (@daily, @every 1h30m) and a "TZ=Area/City" prefix (defaults to
// the server's local time).
type Spec struct {
	ID       string
	Source   string
	Provider string
	Name     string
	Interval time.Duration
	Cron     string
}

type TaskFunc func(ctx context.Context) error

// StateStore persists user-controlled task state (enabled/disabled) so it
// survives restarts. An empty implementation keeps everything in memory.
type StateStore interface {
	GetEnabled(id string) (bool, error)
	SetEnabled(id string, enabled bool) error
}

type entry struct {
	spec     Spec
	fn       TaskFunc
	schedule cron.Schedule
	enabled  bool
	running  bool
	nextRun  time.Time
	lastRun  *time.Time
	lastErr  string
}

// nextAfter returns the next scheduled run strictly after t: the following
// cron occurrence for cron tasks, t+Interval for interval tasks.
func (e *entry) nextAfter(t time.Time) time.Time {
	if e.schedule != nil {
		return e.schedule.Next(t)
	}
	return t.Add(e.spec.Interval)
}

// Scheduler is the central task registry and dispatcher. It uses an adaptive
// timer: the main loop sleeps exactly until the earliest next run among all
// enabled idle tasks, and is woken by a signal whenever the schedule changes
// (register/enable/disable/run-now/task completion).
type Scheduler struct {
	mu    sync.RWMutex
	tasks map[string]*entry
	store StateStore
	wake  chan struct{}
	// baseCtx is the scheduler lifecycle context set by Start; manually
	// triggered runs (RunNow) inherit it so they are cancelled on shutdown.
	baseCtx context.Context
	// stopped rejects new manual runs once Start exited or Shutdown began.
	// Every run goroutine is tracked by wg so Shutdown can join them.
	stopped bool
	wg      sync.WaitGroup
}

func NewScheduler(store StateStore) *Scheduler {
	return &Scheduler{
		tasks: make(map[string]*entry),
		store: store,
		wake:  make(chan struct{}, 1),
	}
}

// Register adds a task or updates an existing one (idempotent). Runtime state
// (enabled, last run) is preserved across updates; a schedule change (interval
// or cron expression) resets the next run. An invalid spec returns an error
// and leaves the registry unchanged.
func (s *Scheduler) Register(spec Spec, fn TaskFunc) error {
	if spec.ID == "" || fn == nil {
		return fmt.Errorf("id and fn are required")
	}

	var schedule cron.Schedule
	switch {
	case spec.Interval > 0 && spec.Cron == "":
		// fixed interval
	case spec.Interval == 0 && spec.Cron != "":
		sched, err := cron.ParseStandard(spec.Cron)
		if err != nil {
			return fmt.Errorf("invalid cron expression %q: %w", spec.Cron, err)
		}
		schedule = sched
	default:
		return fmt.Errorf("exactly one of Interval or Cron must be set")
	}

	s.mu.Lock()
	now := time.Now()
	if e, ok := s.tasks[spec.ID]; ok {
		changed := e.spec.Interval != spec.Interval || e.spec.Cron != spec.Cron
		e.spec = spec
		e.fn = fn
		e.schedule = schedule
		if changed && e.enabled {
			e.nextRun = e.nextAfter(now)
		}
		s.mu.Unlock()
		s.signal()
		logger.Info("[task] updated: %s (%s, %s)", spec.ID, spec.Name, spec.scheduleDesc())
		return nil
	}

	enabled := true
	if s.store != nil {
		if v, err := s.store.GetEnabled(spec.ID); err != nil {
			// A failed read must not silently revert an explicitly disabled
			// task to enabled: fail safe by registering as disabled until an
			// admin (or a healthy store) enables it again.
			logger.Error("[task] read enabled state for %s failed: %v; registering as disabled", spec.ID, err)
			enabled = false
		} else {
			enabled = v
		}
	}
	e := &entry{
		spec:     spec,
		fn:       fn,
		schedule: schedule,
		enabled:  enabled,
		nextRun:  now.Add(spec.Interval),
	}
	if schedule != nil {
		e.nextRun = schedule.Next(now)
	}
	s.tasks[spec.ID] = e
	s.mu.Unlock()
	s.signal()
	logger.Info("[task] registered: %s (%s, %s, enabled=%v)", spec.ID, spec.Name, spec.scheduleDesc(), enabled)
	return nil
}

// scheduleDesc returns a human-readable schedule description for logging.
func (s Spec) scheduleDesc() string {
	if s.Cron != "" {
		return "cron " + s.Cron
	}
	return "every " + s.Interval.String()
}

// Get returns a single registered task by ID, or ErrNotFound when unknown.
func (s *Scheduler) Get(id string) (domain.ScheduledTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.tasks[id]
	if !ok {
		return domain.ScheduledTask{}, ErrNotFound
	}
	return s.view(e), nil
}

// List returns every registered task sorted by next run (ascending, tasks
// without a next run last).
func (s *Scheduler) List() []domain.ScheduledTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.ScheduledTask, 0, len(s.tasks))
	for _, e := range s.tasks {
		out = append(out, s.view(e))
	}
	sort.Slice(out, func(i, j int) bool {
		ni, nj := out[i].NextRun, out[j].NextRun
		switch {
		case ni == nil && nj == nil:
			return out[i].ID < out[j].ID
		case ni == nil:
			return false
		case nj == nil:
			return true
		default:
			return ni.Before(*nj)
		}
	})
	return out
}

// RunNow triggers one immediate execution regardless of the schedule (manual
// trigger, allowed even for disabled tasks). The run inherits the scheduler
// lifecycle context so it is cancelled on shutdown.
func (s *Scheduler) RunNow(id string) error {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return ErrStopped
	}
	e, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	if e.running {
		s.mu.Unlock()
		return ErrAlreadyRunning
	}
	e.running = true
	ctx := s.baseCtx
	now := time.Now()
	s.wg.Add(1)
	s.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		defer s.wg.Done()
		s.run(ctx, e, now)
	}()
	return nil
}

// Remove deletes a registered task (e.g. a plugin unloaded its schedules).
// Removing a missing task is not an error.
func (s *Scheduler) Remove(id string) {
	s.mu.Lock()
	_, ok := s.tasks[id]
	if ok {
		delete(s.tasks, id)
	}
	s.mu.Unlock()
	if ok {
		s.signal()
		logger.Info("[task] removed: %s", id)
	}
}

// SetEnabled enables or disables a task and persists the state. Disabling
// clears the next run; enabling reschedules from now. The check, in-memory
// update and persist all happen under one lock so concurrent calls cannot
// leave the persisted state inconsistent with the in-memory state.
func (s *Scheduler) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	e, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	if e.enabled == enabled {
		s.mu.Unlock()
		return nil
	}
	if s.store != nil {
		if err := s.store.SetEnabled(id, enabled); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	e.enabled = enabled
	if enabled && !e.running {
		e.nextRun = e.nextAfter(time.Now())
	}
	s.mu.Unlock()
	s.signal()
	logger.Info("[task] %s %s", id, map[bool]string{true: "enabled", false: "disabled"}[enabled])
	return nil
}

// Start runs the dispatch loop until ctx is cancelled. All due tasks are
// started concurrently; each runs in its own goroutine and its completion
// wakes the loop so the sleep target is recomputed. Once the loop exits no
// further manual runs are accepted (RunNow returns ErrStopped).
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.baseCtx = ctx
	s.mu.Unlock()
	defer s.markStopped()
	for {
		now := time.Now()
		var due []*entry
		var earliest time.Time

		s.mu.Lock()
		if s.stopped {
			s.mu.Unlock()
			return
		}
		for _, e := range s.tasks {
			if !e.enabled || e.running {
				continue
			}
			if !e.nextRun.After(now) {
				due = append(due, e)
				e.running = true
			} else if earliest.IsZero() || e.nextRun.Before(earliest) {
				earliest = e.nextRun
			}
		}
		s.wg.Add(len(due))
		s.mu.Unlock()

		for _, e := range due {
			go func(e *entry) {
				defer s.wg.Done()
				s.run(ctx, e, now)
			}(e)
		}

		var timerC <-chan time.Time
		var timer *time.Timer
		if !earliest.IsZero() {
			timer = time.NewTimer(time.Until(earliest))
			timerC = timer.C
		}

		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-s.wake:
			if timer != nil {
				timer.Stop()
			}
		case <-timerC:
		}
	}
}

// markStopped rejects new manual runs. The mutex also orders against any
// in-flight RunNow so Shutdown's wg.Wait observes every accepted run.
func (s *Scheduler) markStopped() {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
}

// Shutdown stops accepting new runs and blocks until every in-flight run
// goroutine has finished. Call it after the dispatch loop's context is
// cancelled (which makes Start exit) and no more RunNow calls can arrive.
func (s *Scheduler) Shutdown() {
	s.markStopped()
	s.wg.Wait()
}

// run executes one task instance and updates the entry state. The next run
// is anchored to the trigger time (drift-free); a run that outlasts its
// interval reschedules from completion instead of queueing a catch-up burst.
func (s *Scheduler) run(ctx context.Context, e *entry, trigger time.Time) {
	start := time.Now()
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("[task] %s panicked: %v", e.spec.ID, r)
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return e.fn(ctx)
	}()

	finish := time.Now()
	next := e.nextAfter(trigger)
	if next.Before(finish) {
		// The run outlasted its own schedule: skip the missed occurrence
		// (no catch-up burst) and target the next one after completion.
		next = e.nextAfter(finish)
	}

	s.mu.Lock()
	e.running = false
	e.lastRun = &finish
	if err != nil {
		e.lastErr = err.Error()
		logger.Error("[task] %s failed after %v: %v", e.spec.ID, finish.Sub(start), err)
	} else {
		e.lastErr = ""
		logger.Info("[task] %s completed in %v", e.spec.ID, finish.Sub(start))
	}
	e.nextRun = next
	s.mu.Unlock()
	s.signal()
}

func (s *Scheduler) view(e *entry) domain.ScheduledTask {
	t := domain.ScheduledTask{
		ID:        e.spec.ID,
		Source:    e.spec.Source,
		Provider:  e.spec.Provider,
		Name:      e.spec.Name,
		Enabled:   e.enabled,
		LastRun:   e.lastRun,
		LastError: e.lastErr,
	}
	if e.spec.Cron != "" {
		t.Cron = e.spec.Cron
	} else {
		t.IntervalSec = int64(e.spec.Interval.Seconds())
	}
	switch {
	case e.running:
		// A manual trigger on a disabled task is still an execution: the
		// running state must take precedence so the UI reflects reality.
		t.Status = StatusRunning
	case !e.enabled:
		t.Status = StatusDisabled
	default:
		t.Status = StatusIdle
	}
	if e.enabled && !e.running {
		nr := e.nextRun
		t.NextRun = &nr
	}
	return t
}

func (s *Scheduler) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
