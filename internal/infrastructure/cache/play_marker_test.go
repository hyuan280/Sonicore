package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/config"
)

func newTestPlayMarkers(t *testing.T) (*PlayMarkerStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	vk := NewValkey(config.RedisConfig{Host: mr.Host(), Port: mr.Server().Addr().Port, KeyPrefix: "test:"})
	t.Cleanup(func() { vk.Close() })
	return NewPlayMarkerStore(vk), mr
}

func TestPlayMarkerAllowOncePerWindow(t *testing.T) {
	m, mr := newTestPlayMarkers(t)
	ctx := context.Background()
	window := time.Minute

	ok, err := m.Allow(ctx, "play", "u-1", "t-1", "token-1", window)
	require.NoError(t, err)
	assert.True(t, ok, "first play in the window is allowed")

	// Any later attempt in the same window is rejected, regardless of token.
	ok, err = m.Allow(ctx, "play", "u-1", "t-1", "token-2", window)
	require.NoError(t, err)
	assert.False(t, ok, "repeat within the window is rejected")

	mr.FastForward(window + time.Second)
	ok, err = m.Allow(ctx, "play", "u-1", "t-1", "token-3", window)
	require.NoError(t, err)
	assert.True(t, ok, "play after the window is allowed again")
}

func TestPlayMarkerKindsAreIndependent(t *testing.T) {
	m, _ := newTestPlayMarkers(t)
	ctx := context.Background()

	ok, err := m.Allow(ctx, "play", "u-1", "t-1", "token-1", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = m.Allow(ctx, "playc", "u-1", "t-1", "token-1", time.Minute)
	require.NoError(t, err)
	assert.True(t, ok, "completion marker is independent of the play marker")
}

func TestListenProgressAccumulatesRealTime(t *testing.T) {
	m, mr := newTestPlayMarkers(t)
	ctx := context.Background()
	base := time.Now()
	mr.SetTime(base)

	// First report only establishes the baseline.
	session, listened, err := m.ListenProgress(ctx, "u-1", "t-1", "tok", 0, time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "tok", session)
	assert.InDelta(t, 0, listened, 0.001)

	// 15s later, having advanced 15s, credits 15s. A new candidate token does
	// not replace the listen's session.
	mr.SetTime(base.Add(15 * time.Second))
	session, listened, err = m.ListenProgress(ctx, "u-1", "t-1", "tok2", 15, time.Hour)
	require.NoError(t, err)
	assert.Equal(t, "tok", session, "the listen keeps its original session token")
	assert.InDelta(t, 15, listened, 0.001)

	// A forward jump to 150 only credits the 5s of wall-clock that elapsed.
	mr.SetTime(base.Add(20 * time.Second))
	_, listened, err = m.ListenProgress(ctx, "u-1", "t-1", "tok3", 150, time.Hour)
	require.NoError(t, err)
	assert.InDelta(t, 20, listened, 0.001, "a seek credits only elapsed time")
}

func TestListenProgressResumeFromMiddle(t *testing.T) {
	m, mr := newTestPlayMarkers(t)
	ctx := context.Background()
	base := time.Now()
	mr.SetTime(base)

	// Resuming at 120s earns no credit on the first report.
	_, listened, err := m.ListenProgress(ctx, "u-1", "t-1", "tok", 120, time.Hour)
	require.NoError(t, err)
	assert.InDelta(t, 0, listened, 0.001)

	// Ten seconds of real playback later only 10s is credited.
	mr.SetTime(base.Add(10 * time.Second))
	_, listened, err = m.ListenProgress(ctx, "u-1", "t-1", "tok", 130, time.Hour)
	require.NoError(t, err)
	assert.InDelta(t, 10, listened, 0.001)
}

func TestListenProgressRotatesWindow(t *testing.T) {
	m, mr := newTestPlayMarkers(t)
	ctx := context.Background()
	base := time.Now()
	window := time.Minute
	mr.SetTime(base)

	session, listened, err := m.ListenProgress(ctx, "u-1", "t-1", "tok-1", 0, window)
	require.NoError(t, err)
	require.Equal(t, "tok-1", session)
	require.InDelta(t, 0, listened, 0.001)

	// Within the window the session is stable and progress accumulates.
	mr.SetTime(base.Add(20 * time.Second))
	session, listened, err = m.ListenProgress(ctx, "u-1", "t-1", "tok-2", 20, window)
	require.NoError(t, err)
	assert.Equal(t, "tok-1", session, "session stays for the rest of the window")
	assert.InDelta(t, 20, listened, 0.001)

	// Crossing the window rotates the session and resets the accumulator, so
	// the next window's play/complete get a fresh database dedupe key.
	mr.SetTime(base.Add(window + time.Second))
	session, listened, err = m.ListenProgress(ctx, "u-1", "t-1", "tok-3", 0, window)
	require.NoError(t, err)
	assert.Equal(t, "tok-3", session, "session rotates at the window boundary")
	assert.InDelta(t, 0, listened, 0.001, "accumulator resets for the new window")
}
