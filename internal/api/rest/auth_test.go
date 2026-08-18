package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/config"
	"github.com/sonicore/server/internal/infrastructure/auth"
	"github.com/sonicore/server/internal/infrastructure/cache"
)

func contextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, middleware.CtxUserID, userID)
}

func contextWithRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, middleware.CtxUserRole, role)
}

func newAuthHandler(t *testing.T) (*AuthHandler, *sql.DB, sqlmock.Sqlmock, *miniredis.Miniredis) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	mr := miniredis.RunT(t)
	vk := cache.NewValkey(config.RedisConfig{Host: mr.Host(), Port: mr.Server().Addr().Port, KeyPrefix: "test:"})
	t.Cleanup(func() { vk.Close() })

	jwtService := auth.NewJWTService("test-secret", "1h")
	handler := NewAuthHandler(db, jwtService,
		cache.NewTokenStore(vk),
		cache.NewSessionStore(vk),
		24*time.Hour)
	return handler, db, mock, mr
}

func doJSONRequest(handler http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "test-agent")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func expectUserFindByUsername(mock sqlmock.Sqlmock, username, email, hash string, role string) {
	rows := sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
		AddRow("u-001", username, email, hash, role, time.Now(), time.Now())
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE username = \$1`).
		WithArgs(username).
		WillReturnRows(rows)
}

func TestAuthRegisterSuccess(t *testing.T) {
	handler, _, mock, mr := newAuthHandler(t)

	mock.ExpectQuery(`SELECT value FROM server_settings WHERE key=\$1`).
		WithArgs("allow_registration").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("true"))
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE username = \$1`).
		WithArgs("alice").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE email = \$1`).
		WithArgs("alice@example.com").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rec := doJSONRequest(handler.Register, http.MethodPost, "/api/auth/register",
		`{"username":"alice","email":"alice@example.com","password":"secret1"}`)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var body authResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "alice", body.Username)
	assert.Equal(t, "super_admin", string(body.Role), "first registered user becomes super admin")
	assert.NotEmpty(t, body.Token)
	assert.NotEmpty(t, body.RefreshToken)
	assert.NotEmpty(t, body.SessionToken)
	assert.NotEmpty(t, body.UserID)

	// refresh token hash + user set + session key
	assert.Len(t, mr.Keys(), 3)
}

func TestAuthRegisterSecondUserIsRegular(t *testing.T) {
	handler, _, mock, _ := newAuthHandler(t)

	mock.ExpectQuery(`SELECT value FROM server_settings WHERE key=\$1`).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("true"))
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE username = \$1`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE email = \$1`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rec := doJSONRequest(handler.Register, http.MethodPost, "/api/auth/register",
		`{"username":"bob","email":"bob@example.com","password":"secret1"}`)
	assert.Equal(t, http.StatusCreated, rec.Code)

	var body authResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "user", string(body.Role))
}

func TestAuthRegisterInvalidBody(t *testing.T) {
	handler, _, _, _ := newAuthHandler(t)
	rec := doJSONRequest(handler.Register, http.MethodPost, "/api/auth/register", `not-json`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":4`)
}

func TestAuthRegisterMissingFields(t *testing.T) {
	handler, _, _, _ := newAuthHandler(t)
	rec := doJSONRequest(handler.Register, http.MethodPost, "/api/auth/register", `{"username":"","email":"","password":""}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":116`)
}

func TestAuthRegisterShortPassword(t *testing.T) {
	handler, _, _, _ := newAuthHandler(t)
	rec := doJSONRequest(handler.Register, http.MethodPost, "/api/auth/register",
		`{"username":"a","email":"a@b.c","password":"12345"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":105`)
}

func TestAuthRegisterDisabled(t *testing.T) {
	handler, _, mock, _ := newAuthHandler(t)

	mock.ExpectQuery(`SELECT value FROM server_settings WHERE key=\$1`).
		WithArgs("allow_registration").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("false"))

	rec := doJSONRequest(handler.Register, http.MethodPost, "/api/auth/register",
		`{"username":"a","email":"a@b.c","password":"secret1"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":102`)
}

func TestAuthRegisterDuplicateUsername(t *testing.T) {
	handler, _, mock, _ := newAuthHandler(t)

	mock.ExpectQuery(`SELECT value FROM server_settings WHERE key=\$1`).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("true"))
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE username = \$1`).
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
			AddRow("u-1", "alice", "a@b.c", "h", "user", time.Now(), time.Now()))

	rec := doJSONRequest(handler.Register, http.MethodPost, "/api/auth/register",
		`{"username":"alice","email":"a@b.c","password":"secret1"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":103`)
}

func TestAuthRegisterDuplicateEmail(t *testing.T) {
	handler, _, mock, _ := newAuthHandler(t)

	mock.ExpectQuery(`SELECT value FROM server_settings WHERE key=\$1`).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("true"))
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE username = \$1`).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE email = \$1`).
		WithArgs("a@b.c").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
			AddRow("u-1", "other", "a@b.c", "h", "user", time.Now(), time.Now()))

	rec := doJSONRequest(handler.Register, http.MethodPost, "/api/auth/register",
		`{"username":"alice","email":"a@b.c","password":"secret1"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":104`)
}

func TestAuthLoginSuccess(t *testing.T) {
	handler, _, mock, mr := newAuthHandler(t)

	hash, err := auth.HashPassword("secret1")
	require.NoError(t, err)
	expectUserFindByUsername(mock, "alice", "alice@example.com", hash, "user")

	rec := doJSONRequest(handler.Login, http.MethodPost, "/api/auth/login",
		`{"username":"alice","password":"secret1"}`)
	assert.Equal(t, http.StatusOK, rec.Code)

	var body authResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "u-001", body.UserID)
	assert.NotEmpty(t, body.Token)
	assert.Len(t, mr.Keys(), 3)
}

func TestAuthLoginWrongPassword(t *testing.T) {
	handler, _, mock, _ := newAuthHandler(t)

	hash, _ := auth.HashPassword("secret1")
	expectUserFindByUsername(mock, "alice", "alice@example.com", hash, "user")

	rec := doJSONRequest(handler.Login, http.MethodPost, "/api/auth/login",
		`{"username":"alice","password":"wrong"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":101`)
}

func TestAuthLoginUserNotFound(t *testing.T) {
	handler, _, mock, _ := newAuthHandler(t)

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE username = \$1`).
		WithArgs("ghost").
		WillReturnError(sql.ErrNoRows)

	rec := doJSONRequest(handler.Login, http.MethodPost, "/api/auth/login",
		`{"username":"ghost","password":"whatever"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthLoginMissingFields(t *testing.T) {
	handler, _, _, _ := newAuthHandler(t)
	rec := doJSONRequest(handler.Login, http.MethodPost, "/api/auth/login", `{"username":"a"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":100`)
}

func TestAuthRefreshSuccess(t *testing.T) {
	handler, _, mock, mr := newAuthHandler(t)
	ctx := context.Background()

	// seed a refresh token directly
	ts := cache.NewTokenStore(cache.NewValkey(config.RedisConfig{Host: mr.Host(), Port: mr.Server().Addr().Port, KeyPrefix: "test:"}))
	refreshToken := ts.Generate()
	require.NoError(t, ts.Store(ctx, "u-001", refreshToken, time.Hour))

	mock.ExpectQuery(`SELECT id, username, email, password_hash, role, created_at, updated_at FROM users WHERE id = \$1`).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "username", "email", "password_hash", "role", "created_at", "updated_at"}).
			AddRow("u-001", "alice", "a@b.c", "h", "user", time.Now(), time.Now()))

	rec := doJSONRequest(handler.Refresh, http.MethodPost, "/api/auth/refresh",
		`{"refresh_token":"`+refreshToken+`"}`)
	assert.Equal(t, http.StatusOK, rec.Code)

	var body authResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "u-001", body.UserID)
	assert.NotEqual(t, refreshToken, body.RefreshToken, "refresh token should rotate")
}

func TestAuthRefreshMissingToken(t *testing.T) {
	handler, _, _, _ := newAuthHandler(t)
	rec := doJSONRequest(handler.Refresh, http.MethodPost, "/api/auth/refresh", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":106`)
}

func TestAuthRefreshInvalidToken(t *testing.T) {
	handler, _, _, _ := newAuthHandler(t)
	rec := doJSONRequest(handler.Refresh, http.MethodPost, "/api/auth/refresh", `{"refresh_token":"bogus"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":107`)
}

func TestAuthLogout(t *testing.T) {
	handler, _, _, mr := newAuthHandler(t)

	// seed tokens for the user
	vk := cache.NewValkey(config.RedisConfig{Host: mr.Host(), Port: mr.Server().Addr().Port, KeyPrefix: "test:"})
	ts := cache.NewTokenStore(vk)
	token := ts.Generate()
	require.NoError(t, ts.Store(context.Background(), "u-001", token, time.Hour))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("User-Agent", "test-agent")
	rec := httptest.NewRecorder()

	handler.Logout(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "logged out")

	_, err := ts.Validate(context.Background(), token)
	require.Error(t, err, "token should be revoked")
}

func TestAuthLogoutNoUser(t *testing.T) {
	handler, _, _, _ := newAuthHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	handler.Logout(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":1`)
}
