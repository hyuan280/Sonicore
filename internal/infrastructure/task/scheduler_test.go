package task

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noopFn() TaskFunc {
	return func(ctx context.Context) error { return nil }
}

type memStore struct {
	mu      sync.Mutex
	enabled map[string]bool
}

func (m *memStore) GetEnabled(id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.enabled[id]; ok {
		return v, nil
	}
	return true, nil
}

func (m *memStore) SetEnabled(id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled[id] = enabled
	return nil
}

// failStore lets tests force GetEnabled/SetEnabled errors.
type failStore struct {
	getErr error
	setErr error
}

func (f *failStore) GetEnabled(id string) (bool, error) {
	if f.getErr != nil {
		return false, f.getErr
	}
	return true, nil
}

func (f *failStore) SetEnabled(id string, enabled bool) error {
	return f.setErr
}

func TestRegisterValidation(t *testing.T) {
	s := NewScheduler(nil)
	fn := noopFn()

	err := s.Register(Spec{ID: "neither", Name: "neither"}, fn)
	require.Error(t, err, "neither interval nor cron must fail")

	err = s.Register(Spec{ID: "both", Name: "both", Interval: time.Minute, Cron: "0 3 * * *"}, fn)
	require.Error(t, err, "both interval and cron must fail")

	err = s.Register(Spec{ID: "badcron", Name: "bad", Cron: "not a cron"}, fn)
	require.Error(t, err, "invalid cron expression must fail")

	err = s.Register(Spec{ID: "nofn", Name: "nofn", Interval: time.Minute}, nil)
	require.Error(t, err, "nil fn must fail")

	require.Empty(t, s.List(), "failed registrations must not leave entries")

	require.NoError(t, s.Register(Spec{ID: "interval", Name: "i", Interval: time.Minute}, fn))
	require.NoError(t, s.Register(Spec{ID: "cron", Name: "c", Cron: "0 3 * * *"}, fn))
	require.Len(t, s.List(), 2)
}

func TestIntervalNextRun(t *testing.T) {
	s := NewScheduler(nil)
	before := time.Now()
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, noopFn()))

	tasks := s.List()
	require.Len(t, tasks, 1)
	require.NotNil(t, tasks[0].NextRun)
	assert.EqualValues(t, 3600, tasks[0].IntervalSec)
	assert.Empty(t, tasks[0].Cron)
	got := *tasks[0].NextRun
	assert.True(t, got.After(before))
	assert.True(t, got.Before(before.Add(time.Hour+time.Minute)),
		"next run should be roughly one hour out, got %v", got)
}

func TestCronNextRun(t *testing.T) {
	s := NewScheduler(nil)
	before := time.Now()
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Cron: "0 3 * * *"}, noopFn()))

	tasks := s.List()
	require.Len(t, tasks, 1)
	require.NotNil(t, tasks[0].NextRun)
	assert.Empty(t, tasks[0].IntervalSec)
	assert.Equal(t, "0 3 * * *", tasks[0].Cron)

	got := *tasks[0].NextRun
	// Local-time anchor: the run must land on the next 03:00:00 local.
	assert.Equal(t, 3, got.Hour())
	assert.Equal(t, 0, got.Minute())
	assert.Equal(t, 0, got.Second())
	assert.Equal(t, 0, got.Nanosecond())
	assert.True(t, got.After(before))
	assert.True(t, got.Before(before.Add(25*time.Hour)))
}

func TestCronNextRunUTC(t *testing.T) {
	s := NewScheduler(nil)
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Cron: "TZ=UTC 30 4 * * *"}, noopFn()))

	tasks := s.List()
	require.Len(t, tasks, 1)
	require.NotNil(t, tasks[0].NextRun)
	got := tasks[0].NextRun.UTC()
	assert.Equal(t, 4, got.Hour())
	assert.Equal(t, 30, got.Minute())
}

func TestCronEveryDescriptor(t *testing.T) {
	s := NewScheduler(nil)
	before := time.Now()
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Cron: "@every 5m"}, noopFn()))

	tasks := s.List()
	require.Len(t, tasks, 1)
	require.NotNil(t, tasks[0].NextRun)
	got := *tasks[0].NextRun
	assert.True(t, got.After(before.Add(4*time.Minute)))
	assert.True(t, got.Before(before.Add(6*time.Minute)))
}

func TestRegisterUpdateResetsNextRun(t *testing.T) {
	s := NewScheduler(nil)
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, noopFn()))
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t2", Cron: "0 3 * * *"}, noopFn()))

	tasks := s.List()
	require.Len(t, tasks, 1)
	assert.Equal(t, "t2", tasks[0].Name)
	assert.Equal(t, "0 3 * * *", tasks[0].Cron)
	require.NotNil(t, tasks[0].NextRun)
	assert.Equal(t, 3, tasks[0].NextRun.Hour(), "next run must follow the new cron schedule")
}

func TestRunNowExecutesAndReschedules(t *testing.T) {
	s := NewScheduler(nil)
	done := make(chan struct{}, 1)
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, func(ctx context.Context) error {
		done <- struct{}{}
		return nil
	}))

	require.NoError(t, s.RunNow("t"))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not execute after RunNow")
	}

	require.Eventually(t, func() bool {
		for _, task := range s.List() {
			if task.ID == "t" {
				return task.Status == StatusIdle && task.LastRun != nil && task.NextRun != nil
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond)
}

func TestRunNowErrors(t *testing.T) {
	s := NewScheduler(nil)
	require.ErrorIs(t, s.RunNow("missing"), ErrNotFound)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, func(ctx context.Context) error {
		started <- struct{}{}
		<-release
		return nil
	}))
	require.NoError(t, s.RunNow("t"))
	<-started
	require.ErrorIs(t, s.RunNow("t"), ErrAlreadyRunning)
	close(release)
}

func TestSetEnabledPersistsAndReschedules(t *testing.T) {
	store := &memStore{enabled: map[string]bool{}}
	s := NewScheduler(store)
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, noopFn()))

	require.NoError(t, s.SetEnabled("t", false))
	tasks := s.List()
	require.Len(t, tasks, 1)
	assert.Equal(t, StatusDisabled, tasks[0].Status)
	assert.Nil(t, tasks[0].NextRun)
	enabled, _ := store.GetEnabled("t")
	assert.False(t, enabled, "disabled state must be persisted")

	require.NoError(t, s.SetEnabled("t", true))
	tasks = s.List()
	assert.Equal(t, StatusIdle, tasks[0].Status)
	require.NotNil(t, tasks[0].NextRun)
	enabled, _ = store.GetEnabled("t")
	assert.True(t, enabled)

	require.ErrorIs(t, s.SetEnabled("missing", false), ErrNotFound)
}

func TestRegisterHonorsPersistedDisabled(t *testing.T) {
	store := &memStore{enabled: map[string]bool{"t": false}}
	s := NewScheduler(store)
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, noopFn()))

	tasks := s.List()
	require.Len(t, tasks, 1)
	assert.Equal(t, StatusDisabled, tasks[0].Status)
	assert.Nil(t, tasks[0].NextRun)
}

func TestRegisterWithFailingStoreFailsSafe(t *testing.T) {
	// A store read failure must not revert an explicitly disabled task to
	// enabled: the task registers as disabled.
	s := NewScheduler(&failStore{getErr: errors.New("db down")})
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, noopFn()))

	tasks := s.List()
	require.Len(t, tasks, 1)
	assert.Equal(t, StatusDisabled, tasks[0].Status)
	assert.Nil(t, tasks[0].NextRun)
}

func TestSetEnabledStoreFailureKeepsState(t *testing.T) {
	s := NewScheduler(&failStore{setErr: errors.New("db down")})
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, noopFn()))

	err := s.SetEnabled("t", false)
	require.Error(t, err)

	tasks := s.List()
	require.Len(t, tasks, 1)
	assert.True(t, tasks[0].Enabled, "in-memory state must be unchanged when persist fails")
	assert.Equal(t, StatusIdle, tasks[0].Status)
}

func TestRunNowOnDisabledTaskShowsRunning(t *testing.T) {
	s := NewScheduler(nil)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, func(ctx context.Context) error {
		started <- struct{}{}
		<-release
		return nil
	}))
	require.NoError(t, s.SetEnabled("t", false))
	require.NoError(t, s.RunNow("t"))
	<-started

	tasks := s.List()
	require.Len(t, tasks, 1)
	assert.Equal(t, StatusRunning, tasks[0].Status,
		"a manual run on a disabled task must report running, not disabled")
	close(release)
}

func TestListSortsByNextRun(t *testing.T) {
	s := NewScheduler(nil)
	require.NoError(t, s.Register(Spec{ID: "later", Name: "later", Interval: 100 * time.Second}, noopFn()))
	require.NoError(t, s.Register(Spec{ID: "sooner", Name: "sooner", Interval: 10 * time.Second}, noopFn()))

	tasks := s.List()
	require.Len(t, tasks, 2)
	assert.Equal(t, "sooner", tasks[0].ID)
	assert.Equal(t, "later", tasks[1].ID)
	assert.True(t, tasks[0].NextRun.Before(*tasks[1].NextRun))
}

func TestStartTriggersDueTask(t *testing.T) {
	s := NewScheduler(nil)
	ran := make(chan struct{}, 1)
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: 20 * time.Millisecond}, func(ctx context.Context) error {
		select {
		case ran <- struct{}{}:
		default:
		}
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Start(ctx)

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled task did not fire")
	}
}

func TestShutdownWaitsForInFlightRun(t *testing.T) {
	s := NewScheduler(nil)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	finished := make(chan struct{})
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, func(ctx context.Context) error {
		started <- struct{}{}
		<-release
		close(finished)
		return nil
	}))
	require.NoError(t, s.RunNow("t"))
	<-started

	shutdownDone := make(chan struct{})
	go func() {
		s.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		t.Fatal("Shutdown returned while a run was still in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-shutdownDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not return after the run finished")
	}
	select {
	case <-finished:
	default:
		t.Fatal("run did not finish")
	}
}

func TestRunNowRejectedAfterShutdown(t *testing.T) {
	s := NewScheduler(nil)
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, noopFn()))
	s.Shutdown()

	require.ErrorIs(t, s.RunNow("t"), ErrStopped)
}

func TestRunNowRejectedAfterStartCancelled(t *testing.T) {
	s := NewScheduler(nil)
	require.NoError(t, s.Register(Spec{ID: "t", Name: "t", Interval: time.Hour}, noopFn()))

	ctx, cancel := context.WithCancel(context.Background())
	go s.Start(ctx)
	cancel()

	require.Eventually(t, func() bool {
		return errors.Is(s.RunNow("t"), ErrStopped)
	}, 2*time.Second, 10*time.Millisecond)
}
