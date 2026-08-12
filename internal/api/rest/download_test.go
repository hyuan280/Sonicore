package rest

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/download"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failSource always matches and fails instantly, so Manager's async goroutine
// drains deterministically.
type failSource struct{}

func (failSource) Name() string                             { return "fail" }
func (failSource) Match(url string) bool                    { return true }
func (failSource) Resolve(ctx context.Context, url string) (*port.SourceInfo, error) {
	return nil, nil
}
func (failSource) Fetch(ctx context.Context, job *domain.DownloadJob) error {
	return errors.New("download failed")
}

func newDownloadHandler(t *testing.T) (*DownloadHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	mgr := download.NewManager(db)
	mgr.Register(failSource{})
	return NewDownloadHandler(db, mgr), mock
}

func libRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
		"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
		AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now())
}

// expectOwnerCheck mocks the IsOwner FindByID call made by permission checks.
func expectOwnerCheck(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(libRow())
}

func TestDownloadCreateNoUser(t *testing.T) {
	h, _ := newDownloadHandler(t)

	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/download",
		strings.NewReader(`{"url":"https://example.com/x.mp3"}`)))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestDownloadCreateInvalidBody(t *testing.T) {
	h, _ := newDownloadHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/download", strings.NewReader("not-json"))
	rec := httptest.NewRecorder()
	h.Create(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid request body")
}

func TestDownloadCreateEmptyURL(t *testing.T) {
	h, _ := newDownloadHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/download", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "url is required")
}

func TestDownloadCreateForbidden(t *testing.T) {
	h, mock := newDownloadHandler(t)

	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "other", "database", "", nil, 0, 0, 0.0, now(), now()))

	req := httptest.NewRequest(http.MethodPost, "/api/download",
		strings.NewReader(`{"url":"https://x/y.mp3","library_id":"lib-001"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "need contributor")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDownloadCreateSuccess(t *testing.T) {
	h, mock := newDownloadHandler(t)

	// CreateJob INSERT (sync)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO download_jobs`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	// async runJob: FindByID → Update(downloading) → Fetch fails → Update(failed)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM download_jobs WHERE id = $1`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "url", "source", "library_id", "format", "status",
			"progress", "target_path", "metadata", "error", "created_at", "updated_at"}).
			AddRow("job-1", "https://x/y.mp3", "fail", "", "", "pending", 0.0, "", "", "", now(), now()))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE download_jobs SET source=$1, format=$2, status=$3, progress=$4,
		 target_path=$5, metadata=$6, error=$7, updated_at=NOW()
		 WHERE id=$8`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE download_jobs SET source=$1, format=$2, status=$3, progress=$4,
		 target_path=$5, metadata=$6, error=$7, updated_at=NOW()
		 WHERE id=$8`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// fail:// is not matched by the built-in direct source, so failSource wins
	req := httptest.NewRequest(http.MethodPost, "/api/download",
		strings.NewReader(`{"url":"fail://x"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"pending"`)

	var lastErr error
	require.Eventually(t, func() bool {
		lastErr = mock.ExpectationsWereMet()
		return lastErr == nil
	}, 2*time.Second, 10*time.Millisecond, "remaining: %v", lastErr)
}

func TestDownloadList(t *testing.T) {
	h, mock := newDownloadHandler(t)

	// owner short-circuits HasRole → no members query
	expectOwnerCheck(mock)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM download_jobs WHERE library_id = $1 ORDER BY created_at DESC`)).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "url", "source", "library_id", "format", "status",
			"progress", "target_path", "metadata", "error", "created_at", "updated_at"}).
			AddRow("job-1", "u", "direct", "lib-001", "mp3", "completed", 100.0, "/x.mp3", "", "", now(), now()))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/libraries/lib-001/downloads", nil),
		map[string]string{"id": "lib-001"})
	rec := httptest.NewRecorder()
	h.List(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"completed"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDownloadListForbidden(t *testing.T) {
	h, mock := newDownloadHandler(t)

	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnError(sql.ErrNoRows)

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/libraries/lib-001/downloads", nil),
		map[string]string{"id": "lib-001"})
	rec := httptest.NewRecorder()
	h.List(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDownloadGetNotFound(t *testing.T) {
	h, mock := newDownloadHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM download_jobs WHERE id = $1`)).
		WithArgs("nope").
		WillReturnError(sql.ErrNoRows)

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/downloads/nope", nil),
		map[string]string{"jobId": "nope"})
	rec := httptest.NewRecorder()
	h.Get(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDownloadGetForbidden(t *testing.T) {
	h, mock := newDownloadHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM download_jobs WHERE id = $1`)).
		WithArgs("job-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "url", "source", "library_id", "format", "status",
			"progress", "target_path", "metadata", "error", "created_at", "updated_at"}).
			AddRow("job-1", "u", "direct", "lib-001", "mp3", "pending", 0.0, "", "", "", now(), now()))
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnError(sql.ErrNoRows)

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/downloads/job-1", nil),
		map[string]string{"jobId": "job-1"})
	rec := httptest.NewRecorder()
	h.Get(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDownloadGetSuccess(t *testing.T) {
	h, mock := newDownloadHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM download_jobs WHERE id = $1`)).
		WithArgs("job-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "url", "source", "library_id", "format", "status",
			"progress", "target_path", "metadata", "error", "created_at", "updated_at"}).
			AddRow("job-1", "u", "direct", "lib-001", "mp3", "completed", 100.0, "/x.mp3", "", "", now(), now()))
	expectOwnerCheck(mock)

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/downloads/job-1", nil),
		map[string]string{"jobId": "job-1"})
	rec := httptest.NewRecorder()
	h.Get(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"id":"job-1"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDownloadCancel(t *testing.T) {
	h, mock := newDownloadHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM download_jobs WHERE id = $1`)).
		WithArgs("job-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "url", "source", "library_id", "format", "status",
			"progress", "target_path", "metadata", "error", "created_at", "updated_at"}).
			AddRow("job-1", "u", "direct", "lib-001", "mp3", "downloading", 50.0, "", "", "", now(), now()))
	expectOwnerCheck(mock)

	req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/downloads/job-1/cancel", nil),
		map[string]string{"jobId": "job-1"})
	rec := httptest.NewRecorder()
	h.Cancel(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "cancelled")
	require.NoError(t, mock.ExpectationsWereMet())
}
