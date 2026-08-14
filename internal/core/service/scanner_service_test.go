package service

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestScannerService(t *testing.T) (*ScannerService, *sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	s := NewScannerService(db, t.TempDir(), t.TempDir(), metadata.MBConfig{}, nil)
	return s, db, mock
}

func TestGetProgressNil(t *testing.T) {
	s, _, _ := newTestScannerService(t)
	assert.Nil(t, s.GetProgress("lib-1"))
}

func TestGetProgressReturnsStored(t *testing.T) {
	s, _, _ := newTestScannerService(t)

	s.activeScan["lib-1"] = &ScanProgress{LibraryID: "lib-1", Status: "running", TotalFiles: 10}
	s.activeScan["lib-2"] = &ScanProgress{LibraryID: "lib-2", Status: "completed"}

	p := s.GetProgress("lib-1")
	require.NotNil(t, p)
	assert.Equal(t, "running", p.Status)
	assert.Equal(t, 10, p.TotalFiles)

	p2 := s.GetProgress("lib-2")
	require.NotNil(t, p2)
	assert.Equal(t, "completed", p2.Status)

	assert.Nil(t, s.GetProgress("lib-3"))
}

func TestStartScanRejectsDuplicate(t *testing.T) {
	s, _, _ := newTestScannerService(t)

	s.activeScan["lib-1"] = &ScanProgress{LibraryID: "lib-1", Status: "running"}

	err := s.StartScan(t.Context(), "lib-1", "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestSetErrorClearsProgress(t *testing.T) {
	s, _, _ := newTestScannerService(t)
	s.activeScan["lib-1"] = &ScanProgress{LibraryID: "lib-1", Status: "running"}

	s.setError("lib-1", "boom")

	assert.Nil(t, s.GetProgress("lib-1"), "progress entry should be removed on error")
}
