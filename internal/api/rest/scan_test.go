package rest

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/core/service"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func now() time.Time { return time.Now() }

func newScanHandler(t *testing.T) (*ScanHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	svc := service.NewScannerService(db, t.TempDir(), t.TempDir(), metadata.MBConfig{})
	return NewScanHandler(db, svc), mock
}

func scanRequest(method, path, userID string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req = mux.SetURLVars(req, map[string]string{"id": "lib-001"})
	if userID != "" {
		req = req.WithContext(contextWithUserID(req.Context(), userID))
	}
	return req
}

func TestScanStartNoUserForbidden(t *testing.T) {
	h, mock := newScanHandler(t)

	// no user in context → permission check fails without DB round trips
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "other-user", "database", "", nil, 0, 0, 0.0, now(), now()))

	rec := httptest.NewRecorder()
	h.Start(rec, scanRequest(http.MethodPost, "/api/libraries/lib-001/scan", ""))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "need contributor role")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScanStartOwnerAllowed(t *testing.T) {
	h, mock := newScanHandler(t)

	mock.ExpectQuery(`FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))

	// StartScan marks active and spawns an async goroutine; the goroutine's
	// DB traffic is not asserted here (it runs detached).
	rec := httptest.NewRecorder()
	h.Start(rec, scanRequest(http.MethodPost, "/api/libraries/lib-001/scan?mode=overwrite", "u-001"))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), "scan started")
	assert.Contains(t, rec.Body.String(), "overwrite")
}

func TestScanStartAlreadyRunning(t *testing.T) {
	h, mock := newScanHandler(t)

	// the seeded scan's goroutine and the handler's permission check both
	// run FindByID; identical queries, order-independent
	mock.ExpectQuery(`FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))
	mock.ExpectQuery(`FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))

	// keep the goroutine stuck on job creation so the scan stays "running"
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO scan_jobs`)).
		WillDelayFor(5 * time.Second).
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, h.scanner.StartScan(t.Context(), "lib-001", "missing"))

	rec := httptest.NewRecorder()
	h.Start(rec, scanRequest(http.MethodPost, "/api/libraries/lib-001/scan", "u-001"))

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "already running")
}

func TestScanStatusIdle(t *testing.T) {
	h, _ := newScanHandler(t)

	rec := httptest.NewRecorder()
	h.Status(rec, scanRequest(http.MethodGet, "/api/libraries/lib-001/scan", ""))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"idle"`)
}

func TestScanStatusRunning(t *testing.T) {
	h, mock := newScanHandler(t)

	// seed a scan via the public API; keep the goroutine stuck in the
	// (delayed) job INSERT so progress stays queryable
	mock.ExpectQuery(`FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO scan_jobs`)).
		WillDelayFor(5 * time.Second).
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, h.scanner.StartScan(t.Context(), "lib-001", "missing"))
	require.Eventually(t, func() bool {
		p := h.scanner.GetProgress("lib-001")
		return p != nil && p.Status == "running"
	}, time.Second, 5*time.Millisecond)

	rec := httptest.NewRecorder()
	h.Status(rec, scanRequest(http.MethodGet, "/api/libraries/lib-001/scan", ""))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"running"`)
	assert.Contains(t, rec.Body.String(), `"total_files":0`)
	assert.Contains(t, rec.Body.String(), `"scanned":0`)
}

func TestScanStartInvalidModeFallsBackToMissing(t *testing.T) {
	h, mock := newScanHandler(t)

	mock.ExpectQuery(`FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))

	rec := httptest.NewRecorder()
	h.Start(rec, scanRequest(http.MethodPost, "/api/libraries/lib-001/scan?mode=weird", "u-001"))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"mode":"missing"`)
}
