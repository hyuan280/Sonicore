package cache

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/sonicore/server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSessionStore(t *testing.T) (*SessionStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	vk := NewValkey(config.RedisConfig{Host: mr.Host(), Port: mr.Server().Addr().Port, KeyPrefix: "test:"})
	t.Cleanup(func() { vk.Close() })
	return NewSessionStore(vk), mr
}

func TestHashClientInfo(t *testing.T) {
	h1 := hashClientInfo("Mozilla/5.0 (Windows)")
	h2 := hashClientInfo("Mozilla/5.0 (Windows)")
	h3 := hashClientInfo("Mozilla/5.0 (Mac)")

	assert.Len(t, h1, 64, "sha256 hex")
	assert.Equal(t, h1, h2, "deterministic for same input")
	assert.NotEqual(t, h1, h3, "different input different hash")
	assert.NotEqual(t, hashClientInfo(""), hashClientInfo(" "), "whitespace is significant")
}

func TestSessionGenerate(t *testing.T) {
	store, mr := newTestSessionStore(t)
	ctx := context.Background()

	token, err := store.Generate(ctx, "u-001", "my-client")
	require.NoError(t, err)
	assert.Len(t, token, 64, "32 random bytes hex-encoded")

	// stored value is "userID\nclientHash"
	val, err := mr.Get("test:sess:" + token)
	require.NoError(t, err)
	parts := splitSessionValue(t, val)
	assert.Equal(t, "u-001", parts[0])
	assert.Equal(t, hashClientInfo("my-client"), parts[1])
}

func TestSessionGenerateRequiresClientInfo(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	_, err := store.Generate(ctx, "u-001", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client info required")
}

func TestSessionGenerateUniqueTokens(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	a, err := store.Generate(ctx, "u-001", "client")
	require.NoError(t, err)
	b, err := store.Generate(ctx, "u-001", "client")
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestSessionValidate(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	token, err := store.Generate(ctx, "u-001", "client")
	require.NoError(t, err)

	userID, err := store.Validate(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, "u-001", userID)
}

func TestSessionValidateUnknownToken(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	_, err := store.Validate(ctx, "nonexistent-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid session")
}

func TestSessionValidateMalformedValue(t *testing.T) {
	store, mr := newTestSessionStore(t)
	ctx := context.Background()

	mr.Set("test:sess:broken", "no-newline-here")

	_, err := store.Validate(ctx, "broken")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid session format")
}

func TestSessionExtend(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	token, err := store.Generate(ctx, "u-001", "client")
	require.NoError(t, err)

	require.NoError(t, store.Extend(ctx, token, "u-001", "client"))
}

func TestSessionExtendRefreshesTTL(t *testing.T) {
	store, mr := newTestSessionStore(t)
	ctx := context.Background()

	token, err := store.Generate(ctx, "u-001", "client")
	require.NoError(t, err)

	mr.FastForward(4 * time.Minute)

	require.NoError(t, store.Extend(ctx, token, "u-001", "client"))

	// TTL was reset, session still valid well past the original TTL
	mr.FastForward(4 * time.Minute)
	_, err = store.Validate(ctx, token)
	require.NoError(t, err)
}

func TestSessionExtendWrongUser(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	token, err := store.Generate(ctx, "u-001", "client")
	require.NoError(t, err)

	err = store.Extend(ctx, token, "u-002", "client")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user mismatch")
}

func TestSessionExtendClientMismatch(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	token, err := store.Generate(ctx, "u-001", "browser-A")
	require.NoError(t, err)

	err = store.Extend(ctx, token, "u-001", "browser-B")
	require.ErrorIs(t, err, ErrClientMismatch)
}

func TestSessionExtendUnknownToken(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	err := store.Extend(ctx, "missing", "u-001", "client")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")
}

func TestSessionExtendMalformedValue(t *testing.T) {
	store, mr := newTestSessionStore(t)
	ctx := context.Background()

	mr.Set("test:sess:broken", "no-newline")

	err := store.Extend(ctx, "broken", "u-001", "client")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid session format")
}

func TestSessionRevoke(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	token, err := store.Generate(ctx, "u-001", "client")
	require.NoError(t, err)

	require.NoError(t, store.Revoke(ctx, token))

	_, err = store.Validate(ctx, token)
	require.Error(t, err)
}

func TestSessionRevokeUnknownToken(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()
	require.NoError(t, store.Revoke(ctx, "never-issued"))
}

func TestSessionExpiry(t *testing.T) {
	store, mr := newTestSessionStore(t)
	ctx := context.Background()

	token, err := store.Generate(ctx, "u-001", "client")
	require.NoError(t, err)

	mr.FastForward(6 * time.Minute) // TTL is 5 minutes

	_, err = store.Validate(ctx, token)
	require.Error(t, err)
}

func TestSessionTokensIsolated(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	tokenA, err := store.Generate(ctx, "u-001", "client")
	require.NoError(t, err)
	tokenB, err := store.Generate(ctx, "u-002", "client")
	require.NoError(t, err)

	// revoking A must not affect B
	require.NoError(t, store.Revoke(ctx, tokenA))

	userID, err := store.Validate(ctx, tokenB)
	require.NoError(t, err)
	assert.Equal(t, "u-002", userID)
}

func splitSessionValue(t *testing.T, val string) []string {
	t.Helper()
	parts := strings.SplitN(val, "\n", 2)
	require.Len(t, parts, 2, "stored session must be userID\\nclientHash")
	return parts
}
