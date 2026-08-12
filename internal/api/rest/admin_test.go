package rest

import (
	"database/sql"
	"database/sql/driver"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAdminHandler(t *testing.T) (*AdminHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewAdminHandler(db), mock
}

func adminUserRow(id, username, role string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
		AddRow(id, username, username+"@example.com", "h", role, time.Now(), time.Now())
}

func adminUserValues(id, username, role string) []driver.Value {
	return []driver.Value{id, username, username + "@example.com", "h", role, time.Now(), time.Now()}
}

type driverValue2 = interface{}

func TestAdminListUsers(t *testing.T) {
	h, mock := newAdminHandler(t)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users ORDER BY created_at ASC`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
			AddRow(adminUserValues("u-1", "alice", "user")...).
			AddRow(adminUserValues("u-2", "bob", "admin")...))

	rec := httptest.NewRecorder()
	h.ListUsers(rec, httptest.NewRequest(http.MethodGet, "/api/admin/users", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"username":"alice"`)
	assert.Contains(t, rec.Body.String(), `"username":"bob"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminListUsersError(t *testing.T) {
	h, mock := newAdminHandler(t)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users ORDER BY created_at ASC`).
		WillReturnError(sql.ErrConnDone)

	rec := httptest.NewRecorder()
	h.ListUsers(rec, httptest.NewRequest(http.MethodGet, "/api/admin/users", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAdminUpdateUserRoleSuccess(t *testing.T) {
	h, mock := newAdminHandler(t)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("super-1").
		WillReturnRows(adminUserRow("super-1", "root", "super_admin"))
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("u-2").
		WillReturnRows(adminUserRow("u-2", "bob", "user"))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET username=$2, email=$3, password_hash=$4, role=$5, updated_at=$6 WHERE id=$1`)).
		WithArgs("u-2", "bob", "bob@example.com", "h", "admin", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodPut, "/api/admin/users/u-2/role",
		strings.NewReader(`{"role":"admin"}`)), map[string]string{"id": "u-2"})
	req = req.WithContext(contextWithUserID(req.Context(), "super-1"))
	rec := httptest.NewRecorder()
	h.UpdateUserRole(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"role":"admin"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminUpdateUserRoleInvalidRole(t *testing.T) {
	h, _ := newAdminHandler(t)

	req := mux.SetURLVars(httptest.NewRequest(http.MethodPut, "/api/admin/users/u-2/role",
		strings.NewReader(`{"role":"super_admin"}`)), map[string]string{"id": "u-2"})
	rec := httptest.NewRecorder()
	h.UpdateUserRole(rec, req.WithContext(contextWithUserID(req.Context(), "super-1")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "must be admin or user")
}

func TestAdminUpdateUserRoleCannotTouchSuperAdmin(t *testing.T) {
	h, mock := newAdminHandler(t)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("super-1").
		WillReturnRows(adminUserRow("super-1", "root", "super_admin"))
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("u-super-2").
		WillReturnRows(adminUserRow("u-super-2", "other-root", "super_admin"))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodPut, "/api/admin/users/u-super-2/role",
		strings.NewReader(`{"role":"admin"}`)), map[string]string{"id": "u-super-2"})
	rec := httptest.NewRecorder()
	h.UpdateUserRole(rec, req.WithContext(contextWithUserID(req.Context(), "super-1")))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "cannot change super admin")
}

func TestAdminUpdateUserRoleAdminCannotManageAdmin(t *testing.T) {
	h, mock := newAdminHandler(t)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("admin-1").
		WillReturnRows(adminUserRow("admin-1", "a", "admin"))
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("admin-2").
		WillReturnRows(adminUserRow("admin-2", "b", "admin"))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodPut, "/api/admin/users/admin-2/role",
		strings.NewReader(`{"role":"user"}`)), map[string]string{"id": "admin-2"})
	rec := httptest.NewRecorder()
	h.UpdateUserRole(rec, req.WithContext(contextWithUserID(req.Context(), "admin-1")))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "cannot manage other admins")
}

func TestAdminUpdateUserRoleActorNotFound(t *testing.T) {
	h, mock := newAdminHandler(t)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("ghost").
		WillReturnError(sql.ErrNoRows)

	req := mux.SetURLVars(httptest.NewRequest(http.MethodPut, "/api/admin/users/u-2/role",
		strings.NewReader(`{"role":"user"}`)), map[string]string{"id": "u-2"})
	rec := httptest.NewRecorder()
	h.UpdateUserRole(rec, req.WithContext(contextWithUserID(req.Context(), "ghost")))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAdminGetSettings(t *testing.T) {
	h, mock := newAdminHandler(t)

	for _, k := range []string{"allow_registration", "metadata_musicbrainz_enabled", "metadata_musicbrainz_api_url", "metadata_musicbrainz_rate_limit", "subsonic_jukebox_id"} {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM server_settings WHERE key=$1`)).
			WithArgs(k).
			WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("true"))
	}

	rec := httptest.NewRecorder()
	h.GetSettings(rec, httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"allow_registration":true`)
	assert.Contains(t, rec.Body.String(), `"metadata_musicbrainz_enabled":true`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminUpdateSettingsPartial(t *testing.T) {
	h, mock := newAdminHandler(t)

	// only allow_registration is set
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO server_settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value=$2`)).
		WithArgs("allow_registration", "false").
		WillReturnResult(sqlmock.NewResult(0, 1))
	// then 5 reads for the response
	for _, k := range []string{"metadata_musicbrainz_enabled", "metadata_musicbrainz_api_url", "metadata_musicbrainz_rate_limit", "allow_registration", "subsonic_jukebox_id"} {
		mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM server_settings WHERE key=$1`)).
			WithArgs(k).
			WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(""))
	}

	rec := httptest.NewRecorder()
	h.UpdateSettings(rec, httptest.NewRequest(http.MethodPut, "/api/admin/settings",
		strings.NewReader(`{"allow_registration":false}`)))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"allow_registration":false`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdminListDirs(t *testing.T) {
	h, _ := newAdminHandler(t)

	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "zdir"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "adir"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "notadir.txt"), []byte("x"), 0644))

	rec := httptest.NewRecorder()
	h.ListDirs(rec, httptest.NewRequest(http.MethodGet, "/api/admin/dirs?path="+base+"/", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"adir"`)
	assert.Contains(t, rec.Body.String(), `"name":"zdir"`)
	assert.NotContains(t, rec.Body.String(), "notadir", "files excluded")
	// sorted: adir before zdir
	assert.True(t, strings.Index(rec.Body.String(), "adir") < strings.Index(rec.Body.String(), "zdir"))
}

func TestAdminListDirsDefaultPath(t *testing.T) {
	h, _ := newAdminHandler(t)

	rec := httptest.NewRecorder()
	h.ListDirs(rec, httptest.NewRequest(http.MethodGet, "/api/admin/dirs", nil))

	// /opt/sonicore/music may not exist; if it errors the response is 500,
	// otherwise the current dir is reported. Both are acceptable, must not panic.
	assert.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, rec.Code)
}

func TestAdminListDirsNonSlashPathGoesToParent(t *testing.T) {
	h, _ := newAdminHandler(t)

	base := t.TempDir()
	rec := httptest.NewRecorder()
	h.ListDirs(rec, httptest.NewRequest(http.MethodGet, "/api/admin/dirs?path="+filepath.Join(base, "nonexistent-file"), nil))

	// a path without a trailing slash is treated as a file: the parent dir is browsed
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"current":"`+base+`"`)
}

func TestAdminOnlyMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name     string
		role     domain.Role
		wantCode int
	}{
		{"super admin allowed", domain.RoleSuperAdmin, http.StatusOK},
		{"admin allowed", domain.RoleAdmin, http.StatusOK},
		{"user forbidden", domain.RoleUser, http.StatusForbidden},
		{"unknown role forbidden", domain.Role("hacker"), http.StatusForbidden},
		{"no role forbidden", "", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := AdminOnly(next)
			req := httptest.NewRequest(http.MethodGet, "/api/admin/x", nil)
			if tt.role != "" {
				req = req.WithContext(contextWithRole(req.Context(), string(tt.role)))
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCode == http.StatusForbidden {
				assert.Contains(t, rec.Body.String(), "admin access required")
			}
		})
	}
}
