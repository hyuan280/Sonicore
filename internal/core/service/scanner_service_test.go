package service

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/infrastructure/metadata"
)

func newTestScannerService(t *testing.T) (*ScannerService, *sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	s := NewScannerService(db, t.TempDir(), t.TempDir(), metadata.MBConfig{}, nil, nil, false, nil, nil)
	return s, db, mock
}

func TestGetProgressNil(t *testing.T) {
	s, _, _ := newTestScannerService(t)
	assert.Nil(t, s.GetProgress("lib-1"))
}

func TestGetProgressReturnsStored(t *testing.T) {
	s, _, _ := newTestScannerService(t)

	s.activeScan["lib-1"] = &scanRun{progress: &ScanProgress{LibraryID: "lib-1", Status: "running", TotalFiles: 10}}
	s.activeScan["lib-2"] = &scanRun{progress: &ScanProgress{LibraryID: "lib-2", Status: "completed"}}

	p := s.GetProgress("lib-1")
	require.NotNil(t, p)
	assert.Equal(t, "running", p.Status)
	assert.Equal(t, 10, p.TotalFiles)

	p2 := s.GetProgress("lib-2")
	require.NotNil(t, p2)
	assert.Equal(t, "completed", p2.Status)

	assert.Nil(t, s.GetProgress("lib-3"))
}

func TestGetProgressReturnsCopy(t *testing.T) {
	s, _, _ := newTestScannerService(t)
	s.activeScan["lib-1"] = &scanRun{progress: &ScanProgress{LibraryID: "lib-1", Status: "running", Scanned: 1}}

	p := s.GetProgress("lib-1")
	require.NotNil(t, p)
	p.Scanned = 99
	assert.Equal(t, 1, s.activeScan["lib-1"].progress.Scanned, "caller must not mutate the shared entry")
}

func TestStartScanRejectsDuplicate(t *testing.T) {
	s, _, _ := newTestScannerService(t)

	s.activeScan["lib-1"] = &scanRun{progress: &ScanProgress{LibraryID: "lib-1", Status: "running"}}

	err := s.StartScan(t.Context(), "lib-1", "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestCancelScanCancelsAndReturnsStopped(t *testing.T) {
	s, _, _ := newTestScannerService(t)
	cancelled := false
	done := make(chan struct{})
	run := &scanRun{
		progress: &ScanProgress{LibraryID: "lib-1", Status: "running"},
		cancel: func() {
			cancelled = true
			close(done)
		},
		done: done,
	}
	s.activeScan["lib-1"] = run

	assert.True(t, s.CancelScan(t.Context(), "lib-1"))
	assert.True(t, cancelled, "the scan context must be cancelled")

	// The run stays tracked until its goroutine clears it.
	assert.NotNil(t, s.GetProgress("lib-1"))
	s.clearActive("lib-1", run)
	assert.Nil(t, s.GetProgress("lib-1"))

	// No scan for this library → already stopped.
	assert.True(t, s.CancelScan(t.Context(), "lib-missing"))
}

func TestCancelScanTimeoutReturnsFalse(t *testing.T) {
	s, _, _ := newTestScannerService(t)
	old := scanCancelWait
	scanCancelWait = 10 * time.Millisecond
	t.Cleanup(func() { scanCancelWait = old })

	done := make(chan struct{}) // never closed: the scan is stuck
	run := &scanRun{
		progress: &ScanProgress{LibraryID: "lib-1", Status: "running"},
		cancel:   func() {},
		done:     done,
	}
	s.activeScan["lib-1"] = run

	assert.False(t, s.CancelScan(t.Context(), "lib-1"), "a stuck scan must report not-stopped")
	// The entry is kept so a retry (or the UI) still sees it.
	assert.NotNil(t, s.GetProgress("lib-1"))
	s.clearActive("lib-1", run)
}

func TestClearActiveKeepsNewerRun(t *testing.T) {
	s, _, _ := newTestScannerService(t)
	oldRun := &scanRun{progress: &ScanProgress{LibraryID: "lib-1", Status: "running"}}
	newRun := &scanRun{progress: &ScanProgress{LibraryID: "lib-1", Status: "running"}}
	s.activeScan["lib-1"] = newRun

	s.clearActive("lib-1", oldRun)
	assert.Equal(t, newRun, s.activeScan["lib-1"], "an old run must not clear a newer one")

	s.clearActive("lib-1", newRun)
	assert.Nil(t, s.GetProgress("lib-1"))
}

func TestStartScanRejectedWhileDeleting(t *testing.T) {
	s, _, _ := newTestScannerService(t)
	s.BeginLibraryDelete("lib-1")
	t.Cleanup(func() { s.EndLibraryDelete("lib-1") })

	err := s.StartScan(t.Context(), "lib-1", "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "being deleted")
}

func TestSetErrorMarksFailed(t *testing.T) {
	s, _, _ := newTestScannerService(t)
	run := &scanRun{progress: &ScanProgress{LibraryID: "lib-1", Status: "running"}}
	s.activeScan["lib-1"] = run

	s.setError("lib-1", run, "boom")

	p := s.GetProgress("lib-1")
	require.NotNil(t, p)
	assert.Equal(t, "failed", p.Status)

	// The runScan goroutine clears the entry via clearActive on exit.
	s.clearActive("lib-1", run)
	assert.Nil(t, s.GetProgress("lib-1"), "progress entry should be removed on exit")
}
