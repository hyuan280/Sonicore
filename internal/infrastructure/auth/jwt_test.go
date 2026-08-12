package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJWTServiceDefaultExpiration(t *testing.T) {
	svc := NewJWTService("secret", "not-a-duration")
	assert.Equal(t, 72*time.Hour, svc.expiration)
	assert.Equal(t, []byte("secret"), svc.secret)

	svc2 := NewJWTService("s2", "1h")
	assert.Equal(t, time.Hour, svc2.expiration)
}

func TestJWTGenerateValidateRoundTrip(t *testing.T) {
	svc := NewJWTService("test-secret", "1h")

	tests := []struct {
		name     string
		userID   string
		username string
		role     domain.Role
	}{
		{"super admin", "u-001", "root", domain.RoleSuperAdmin},
		{"admin", "u-002", "alice", domain.RoleAdmin},
		{"regular user", "u-003", "bob", domain.RoleUser},
		{"empty fields", "", "", domain.RoleUser},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := svc.Generate(tt.userID, tt.username, tt.role)
			require.NoError(t, err)
			assert.NotEmpty(t, token)

			claims, err := svc.Validate(token)
			require.NoError(t, err)
			assert.Equal(t, tt.userID, claims.UserID)
			assert.Equal(t, tt.username, claims.Username)
			assert.Equal(t, tt.role, claims.Role)
			assert.Greater(t, claims.Exp, time.Now().Unix(), "exp should be in the future")
		})
	}
}

func TestJWTExpiration(t *testing.T) {
	svc := NewJWTService("test-secret", "1h")
	token := svc.generateWithExp(time.Now().Add(-time.Hour))

	_, err := svc.Validate(token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestJWTInvalidSignature(t *testing.T) {
	svc := NewJWTService("test-secret", "1h")

	token, err := svc.Generate("u-001", "alice", domain.RoleAdmin)
	require.NoError(t, err)

	tampered := token[:len(token)-4] + "AAAA"
	_, err = svc.Validate(tampered)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature")
}

func TestJWTDifferentSecret(t *testing.T) {
	issuer := NewJWTService("secret-a", "1h")
	verifier := NewJWTService("secret-b", "1h")

	token, err := issuer.Generate("u-001", "alice", domain.RoleAdmin)
	require.NoError(t, err)

	_, err = verifier.Validate(token)
	require.Error(t, err)
}

func TestJWTMalformedFormat(t *testing.T) {
	svc := NewJWTService("test-secret", "1h")

	valid, err := svc.Generate("u-001", "alice", domain.RoleAdmin)
	require.NoError(t, err)

	tests := []struct {
		name  string
		token string
	}{
		{"empty string", ""},
		{"one part", "abc"},
		{"two parts", "abc.def"},
		{"four parts", valid + ".extra"},
		{"garbage with dots", "a.b.c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Validate(tt.token)
			require.Error(t, err)
		})
	}
}

func TestJWTInvalidPayloadEncoding(t *testing.T) {
	svc := NewJWTService("test-secret", "1h")

	payload := base64.RawURLEncoding.EncodeToString([]byte("not-json"))
	signingInput := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`)) + "." + payload
	token := signingInput + "." + svc.sign(signingInput)

	_, err := svc.Validate(token)
	require.Error(t, err)
}

func TestJWTBadBase64Payload(t *testing.T) {
	svc := NewJWTService("test-secret", "1h")

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	badPayload := "%^^^not-base64"
	signingInput := header + "." + badPayload
	token := signingInput + "." + svc.sign(signingInput)

	_, err := svc.Validate(token)
	require.Error(t, err)
}

func (s *JWTService) generateWithExp(exp time.Time) string {
	claims := Claims{
		UserID:   "u-expired",
		Username: "ghost",
		Role:     domain.RoleUser,
		Exp:      exp.Unix(),
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		panic(fmt.Sprintf("marshal claims: %v", err))
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := header + "." + payload
	return signingInput + "." + s.sign(signingInput)
}
