package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.NotEqual(t, "correct-horse-battery-staple", hash, "hash must not contain plaintext")
}

func TestCheckPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		want     bool
	}{
		{"correct password", "s3cret!pass", true},
		{"wrong password", "wrong-password", false},
		{"empty password against hashed one", "", false},
	}

	hash, err := HashPassword("s3cret!pass")
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CheckPassword(tt.password, hash))
		})
	}
}

func TestCheckPasswordInvalidHash(t *testing.T) {
	assert.False(t, CheckPassword("anything", "not-a-valid-bcrypt-hash"))
	assert.False(t, CheckPassword("anything", ""))
}

func TestHashPasswordRoundTrip(t *testing.T) {
	password := "round-trip-password"
	hash, err := HashPassword(password)
	require.NoError(t, err)
	assert.True(t, CheckPassword(password, hash))
}
