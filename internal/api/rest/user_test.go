package rest

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/sonicore/server/internal/config"
	"github.com/sonicore/server/internal/infrastructure/auth"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newUserHandler(t *testing.T) (*UserHandler, sqlmock.Sqlmock, *miniredis.Miniredis) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	mr := miniredis.RunT(t)
	vk := cache.NewValkey(config.RedisConfig{Host: mr.Host(), Port: mr.Server().Addr().Port, KeyPrefix: "test:"})
	t.Cleanup(func() { vk.Close() })

	return NewUserHandler(db, cache.NewSessionStore(vk), cache.NewTokenStore(vk)), mock, mr
}

func userRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
		AddRow("u-001", "alice", "alice@example.com", "hash", "user", time.Now(), time.Now())
}

func userIDRequest(method, path, body, userID string) *http.Request {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if userID != "" {
		req = req.WithContext(contextWithUserID(req.Context(), userID))
	}
	return req
}

func TestUserMeUnauthorized(t *testing.T) {
	h, _, _ := newUserHandler(t)

	rec := httptest.NewRecorder()
	h.Me(rec, userIDRequest(http.MethodGet, "/api/users/me", "", ""))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUserMeNotFound(t *testing.T) {
	h, mock, _ := newUserHandler(t)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("u-001").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.Me(rec, userIDRequest(http.MethodGet, "/api/users/me", "", "u-001"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUserMeSuccess(t *testing.T) {
	h, mock, _ := newUserHandler(t)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("u-001").
		WillReturnRows(userRows())

	rec := httptest.NewRecorder()
	h.Me(rec, userIDRequest(http.MethodGet, "/api/users/me", "", "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"username":"alice"`)
	assert.Contains(t, rec.Body.String(), `"role":"user"`)
	assert.NotContains(t, rec.Body.String(), `password_hash`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserChangePasswordSuccess(t *testing.T) {
	h, mock, _ := newUserHandler(t)

	hash, err := auth.HashPassword("old-pass")
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
			AddRow("u-001", "alice", "a@b.c", hash, "user", time.Now(), time.Now()))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET username=$2, email=$3, password_hash=$4, role=$5, updated_at=$6 WHERE id=$1`)).
		WithArgs("u-001", "alice", "a@b.c", sqlmock.AnyArg(), "user", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec := httptest.NewRecorder()
	h.ChangePassword(rec, userIDRequest(http.MethodPost, "/api/users/me/password",
		`{"old_password":"old-pass","new_password":"new-pass-123"}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "password updated")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserChangePasswordWrongPassword(t *testing.T) {
	h, mock, _ := newUserHandler(t)

	hash, _ := auth.HashPassword("real-pass")
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
			AddRow("u-001", "alice", "a@b.c", hash, "user", time.Now(), time.Now()))

	rec := httptest.NewRecorder()
	h.ChangePassword(rec, userIDRequest(http.MethodPost, "/api/users/me/password",
		`{"old_password":"wrong","new_password":"new-pass"}` , "u-001"))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "wrong password")
}

func TestUserChangePasswordInvalidBody(t *testing.T) {
	h, _, _ := newUserHandler(t)

	rec := httptest.NewRecorder()
	h.ChangePassword(rec, userIDRequest(http.MethodPost, "/api/users/me/password", "not-json", "u-001"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUserChangePasswordUpdateError(t *testing.T) {
	h, mock, _ := newUserHandler(t)

	hash, _ := auth.HashPassword("old-pass")
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
			AddRow("u-001", "alice", "a@b.c", hash, "user", time.Now(), time.Now()))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE users SET username=$2, email=$3, password_hash=$4, role=$5, updated_at=$6 WHERE id=$1`)).
		WillReturnError(sql.ErrConnDone)

	rec := httptest.NewRecorder()
	h.ChangePassword(rec, userIDRequest(http.MethodPost, "/api/users/me/password",
		`{"old_password":"old-pass","new_password":"new-pass-123"}`, "u-001"))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to update password")
}

func TestUserMeRenewGeneratesSession(t *testing.T) {
	h, _, mr := newUserHandler(t)

	req := userIDRequest(http.MethodPost, "/api/users/me/renew", `{}`, "u-001")
	req.Header.Set("User-Agent", "test-client")
	rec := httptest.NewRecorder()
	h.MeRenew(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"session_token"`)
	assert.NotEmpty(t, mr.Keys())
}

func TestUserMeRenewExtendsExistingSession(t *testing.T) {
	h, _, _ := newUserHandler(t)

	sess, err := h.sessionStore.Generate(context.Background(), "u-001", "same-client")
	require.NoError(t, err)

	req := userIDRequest(http.MethodPost, "/api/users/me/renew",
		`{"session_token":"`+sess+`"}`, "u-001")
	req.Header.Set("User-Agent", "same-client")
	rec := httptest.NewRecorder()
	h.MeRenew(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"ok"`)
	assert.NotContains(t, rec.Body.String(), "session_token", "no new token issued on extend")
}

func TestUserMeRenewClientMismatchRevokesAll(t *testing.T) {
	h, _, mr := newUserHandler(t)
	ctx := context.Background()

	sess, err := h.sessionStore.Generate(ctx, "u-001", "client-a")
	require.NoError(t, err)
	token := h.tokenStore.Generate()
	require.NoError(t, h.tokenStore.Store(ctx, "u-001", token, time.Hour))

	req := userIDRequest(http.MethodPost, "/api/users/me/renew",
		`{"session_token":"`+sess+`"}`, "u-001")
	req.Header.Set("User-Agent", "client-b")
	rec := httptest.NewRecorder()
	h.MeRenew(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "client mismatch")

	_, err = h.sessionStore.Validate(ctx, sess)
	require.Error(t, err, "session revoked")
	_, err = h.tokenStore.Validate(ctx, token)
	require.Error(t, err, "refresh tokens revoked")
	assert.Len(t, mr.Keys(), 0, "all user keys cleaned up")
}

func TestUserMeRenewNoUserAgentFails(t *testing.T) {
	h, _, _ := newUserHandler(t)

	// no session token and no User-Agent → Generate fails
	rec := httptest.NewRecorder()
	h.MeRenew(rec, userIDRequest(http.MethodPost, "/api/users/me/renew", `{}`, "u-001"))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestUserMeRenewUnauthorized(t *testing.T) {
	h, _, _ := newUserHandler(t)

	rec := httptest.NewRecorder()
	h.MeRenew(rec, userIDRequest(http.MethodPost, "/api/users/me/renew", `{}`, ""))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
