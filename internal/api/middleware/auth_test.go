package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/auth"
)

func authMiddlewareHandler(t *testing.T) (func(http.Handler) http.Handler, *auth.JWTService) {
	t.Helper()
	svc := auth.NewJWTService("test-secret", "1h")
	return AuthMiddleware(svc), svc
}

func TestAuthMiddlewareValidToken(t *testing.T) {
	mw, svc := authMiddlewareHandler(t)

	token, err := svc.Generate("u-001", "alice", domain.RoleAdmin)
	require.NoError(t, err)

	var gotUserID, gotUsername, gotRole string
	var hasAdminRole bool
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = GetUserID(r.Context())
		gotUsername = GetUsername(r.Context())
		gotRole = GetUserRole(r.Context())
		hasAdminRole = HasRole(r.Context(), domain.RoleAdmin)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "u-001", gotUserID)
	assert.Equal(t, "alice", gotUsername)
	assert.Equal(t, "admin", gotRole)
	assert.True(t, hasAdminRole)
}

func TestAuthMiddlewareMissingHeader(t *testing.T) {
	mw, _ := authMiddlewareHandler(t)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing authorization header")
}

func TestAuthMiddlewareInvalidFormat(t *testing.T) {
	mw, _ := authMiddlewareHandler(t)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic abc123")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid authorization format")
}

func TestAuthMiddlewareExpiredToken(t *testing.T) {
	mw, _ := authMiddlewareHandler(t)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer expired.token.value")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid or expired token")
}

func TestAuthMiddlewareTamperedToken(t *testing.T) {
	mw, svc := authMiddlewareHandler(t)

	token, err := svc.Generate("u-001", "alice", domain.RoleAdmin)
	require.NoError(t, err)
	tampered := token[:len(token)-4] + "AAAA"

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tampered)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddlewareUnrelatedToken(t *testing.T) {
	_, svc := authMiddlewareHandler(t)
	otherSvc := auth.NewJWTService("different-secret", "1h")
	token, err := otherSvc.Generate("u-001", "alice", domain.RoleUser)
	require.NoError(t, err)

	handler := AuthMiddleware(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
