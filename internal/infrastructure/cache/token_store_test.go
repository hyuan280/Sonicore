package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/sonicore/server/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) (*TokenStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	vk := NewValkey(config.RedisConfig{
		Host:      mr.Host(),
		Port:      mr.Server().Addr().Port,
		KeyPrefix: "test:",
	})
	t.Cleanup(func() { vk.Close() })
	return NewTokenStore(vk), mr
}

func redisKeys(mr *miniredis.Miniredis) []string {
	var keys []string
	for _, k := range mr.Keys() {
		keys = append(keys, k)
	}
	return keys
}

func TestTokenStoreGenerate(t *testing.T) {
	store, _ := newTestStore(t)

	a := store.Generate()
	b := store.Generate()
	assert.Len(t, a, 64, "32 random bytes hex-encoded")
	assert.NotEqual(t, a, b, "tokens should be unique")
}

func TestTokenStoreStoreAndValidate(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	token := store.Generate()
	require.NoError(t, store.Store(ctx, "u-001", token, time.Hour))

	userID, err := store.Validate(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, "u-001", userID)
}

func TestTokenStoreValidateUnknownToken(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	_, err := store.Validate(ctx, "nonexistent-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found or expired")
}

func TestTokenStoreExpiry(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()

	token := store.Generate()
	require.NoError(t, store.Store(ctx, "u-001", token, time.Hour))

	mr.FastForward(2 * time.Hour)

	_, err := store.Validate(ctx, token)
	require.Error(t, err)
}

func TestTokenStoreRevoke(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	token := store.Generate()
	require.NoError(t, store.Store(ctx, "u-001", token, time.Hour))

	require.NoError(t, store.Revoke(ctx, token))

	_, err := store.Validate(ctx, token)
	require.Error(t, err)
}

func TestTokenStoreRevokeUnknownToken(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Revoke(ctx, "never-stored"))
}

func TestTokenStoreRevokeAll(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()

	token1 := store.Generate()
	token2 := store.Generate()
	otherToken := store.Generate()
	require.NoError(t, store.Store(ctx, "u-001", token1, time.Hour))
	require.NoError(t, store.Store(ctx, "u-001", token2, time.Hour))
	require.NoError(t, store.Store(ctx, "u-002", otherToken, time.Hour))

	require.NoError(t, store.RevokeAll(ctx, "u-001"))

	_, err := store.Validate(ctx, token1)
	require.Error(t, err)
	_, err = store.Validate(ctx, token2)
	require.Error(t, err)

	userID, err := store.Validate(ctx, otherToken)
	require.NoError(t, err)
	assert.Equal(t, "u-002", userID)

	keys := redisKeys(mr)
	assert.NotContains(t, keys, "test:user_rt:u-001", "user set should be deleted")
}

func TestTokenStoreRevokeAllNoTokens(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.RevokeAll(ctx, "u-no-tokens"))
}

func TestTokenStoreMultipleUsersIsolated(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	token := store.Generate()
	require.NoError(t, store.Store(ctx, "u-001", token, time.Hour))

	_, err := store.Validate(ctx, store.Generate())
	require.Error(t, err)
}

func TestTokenStoreHashDeterministic(t *testing.T) {
	store, _ := newTestStore(t)
	assert.Equal(t, store.hash("same-token"), store.hash("same-token"))
	assert.NotEqual(t, store.hash("same-token"), store.hash("other-token"))
}

func TestTokenStoreKeysPrefixed(t *testing.T) {
	store, mr := newTestStore(t)
	ctx := context.Background()

	token := store.Generate()
	require.NoError(t, store.Store(ctx, "u-001", token, time.Hour))

	keys := redisKeys(mr)
	assert.Contains(t, keys, "test:rt:"+store.hash(token), "refresh token key should use prefix and hash")
	assert.Contains(t, keys, "test:user_rt:u-001", "user set key should use prefix")
}
