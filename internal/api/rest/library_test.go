package rest

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/infrastructure/player"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLibraryHandler(t *testing.T) (*LibraryHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewLibraryHandler(db, t.TempDir(), t.TempDir(), player.NewEngineManager()), mock
}

func libVarsReq(method, path, id, userID string) *http.Request {
	req := mux.SetURLVars(httptest.NewRequest(method, path, nil), map[string]string{"id": id})
	if userID != "" {
		req = req.WithContext(contextWithUserID(req.Context(), userID))
	}
	return req
}

func TestLibraryCreateUnauthorized(t *testing.T) {
	h, _ := newLibraryHandler(t)

	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/libraries",
		strings.NewReader(`{"name":"Main"}`)))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLibraryCreateInvalidBody(t *testing.T) {
	h, _ := newLibraryHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/libraries", strings.NewReader("not-json"))
	rec := httptest.NewRecorder()
	h.Create(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLibraryCreateMissingName(t *testing.T) {
	h, _ := newLibraryHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/libraries", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "name is required")
}

func TestLibraryCreateInvalidMode(t *testing.T) {
	h, _ := newLibraryHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/libraries",
		strings.NewReader(`{"name":"L","metadata_storage_mode":"bogus"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "must be database, sidecar, or both")
}

func TestLibraryCreateSuccess(t *testing.T) {
	h, mock := newLibraryHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO libraries`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO library_members (library_id, user_id, role, joined_at)`)).
		WithArgs(sqlmock.AnyArg(), "u-001", "owner", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest(http.MethodPost, "/api/libraries",
		strings.NewReader(`{"name":"Main","path":"/music"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"Main"`)
	assert.Contains(t, rec.Body.String(), `"metadata_storage_mode":"database"`, "defaults to database")
	assert.Contains(t, rec.Body.String(), `"owner_id":"u-001"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryCreateDBError(t *testing.T) {
	h, mock := newLibraryHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO libraries`)).
		WillReturnError(sql.ErrConnDone)

	req := httptest.NewRequest(http.MethodPost, "/api/libraries",
		strings.NewReader(`{"name":"Main"}`))
	rec := httptest.NewRecorder()
	h.Create(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to create library")
}

func TestLibraryList(t *testing.T) {
	h, mock := newLibraryHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE lm.user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "Main", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))

	req := httptest.NewRequest(http.MethodGet, "/api/libraries", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"Main"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryListEmpty(t *testing.T) {
	h, mock := newLibraryHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE lm.user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}))

	req := httptest.NewRequest(http.MethodGet, "/api/libraries", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]", strings.TrimSpace(rec.Body.String()), "nil slice normalized to []")
}

func TestLibraryGetForbidden(t *testing.T) {
	h, mock := newLibraryHandler(t)

	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.Get(rec, libVarsReq(http.MethodGet, "/api/libraries/lib-001", "lib-001", "u-001"))

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestLibraryAddMemberSuccess(t *testing.T) {
	h, mock := newLibraryHandler(t)

	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "Main", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO library_members (library_id, user_id, role, joined_at)`)).
		WithArgs("lib-001", "u-002", "viewer", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/lib-001/members",
		strings.NewReader(`{"user_id":"u-002"}`))
	req = mux.SetURLVars(req, map[string]string{"id": "lib-001"})
	rec := httptest.NewRecorder()
	h.AddMember(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"role":"viewer"`, "role defaults to viewer")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryAddMemberInvalidRole(t *testing.T) {
	h, mock := newLibraryHandler(t)

	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "Main", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))

	req := httptest.NewRequest(http.MethodPost, "/api/libraries/lib-001/members",
		strings.NewReader(`{"user_id":"u-002","role":"super"}`))
	req = mux.SetURLVars(req, map[string]string{"id": "lib-001"})
	rec := httptest.NewRecorder()
	h.AddMember(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "role must be admin, contributor, or viewer")
}

func TestLibraryRemoveMemberSuccess(t *testing.T) {
	h, mock := newLibraryHandler(t)

	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "Main", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))
	// permission IsOwner FindByID
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "Main", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM library_members WHERE library_id = $1 AND user_id = $2`)).
		WithArgs("lib-001", "u-002").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodDelete, "/api/libraries/lib-001/members/u-002", nil),
		map[string]string{"id": "lib-001", "userId": "u-002"})
	rec := httptest.NewRecorder()
	h.RemoveMember(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "removed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryRemoveOwnerForbidden(t *testing.T) {
	h, mock := newLibraryHandler(t)

	// permission IsOwner
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "Main", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))
	// handler FindByID to detect owner
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "Main", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodDelete, "/api/libraries/lib-001/members/u-001", nil),
		map[string]string{"id": "lib-001", "userId": "u-001"})
	rec := httptest.NewRecorder()
	h.RemoveMember(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "cannot remove the owner")
}

func TestLibraryUpdateMemberRole(t *testing.T) {
	h, mock := newLibraryHandler(t)

	// permission IsOwner
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "Main", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))
	// handler FindByID
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "Main", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE library_members SET role = $1 WHERE library_id = $2 AND user_id = $3`)).
		WithArgs("contributor", "lib-001", "u-002").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPut, "/api/libraries/lib-001/members/u-002",
		strings.NewReader(`{"role":"contributor"}`))
	req = mux.SetURLVars(req, map[string]string{"id": "lib-001", "userId": "u-002"})
	rec := httptest.NewRecorder()
	h.UpdateMemberRole(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "updated")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryListMembers(t *testing.T) {
	h, mock := newLibraryHandler(t)

	// IsMember → IsOwner short-circuits with owner row
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "Main", "/m", "u-001", "database", "", nil, 0, 0, 0.0, now(), now()))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT library_id, user_id, role, joined_at FROM library_members WHERE library_id = $1`)).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"library_id", "user_id", "role", "joined_at"}).
			AddRow("lib-001", "u-001", "owner", now()).
			AddRow("lib-001", "u-002", "viewer", now()))

	rec := httptest.NewRecorder()
	h.ListMembers(rec, libVarsReq(http.MethodGet, "/api/libraries/lib-001/members", "lib-001", "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"role":"viewer"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryListMembersForbidden(t *testing.T) {
	h, mock := newLibraryHandler(t)

	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.ListMembers(rec, libVarsReq(http.MethodGet, "/api/libraries/lib-001/members", "lib-001", "u-001"))

	assert.Equal(t, http.StatusForbidden, rec.Code)
}
