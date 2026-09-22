package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHeatRateLimitWindow(t *testing.T) {
	assert.Equal(t, 30*time.Second, HeatRateLimitWindow(0), "unknown duration uses the floor")
	assert.Equal(t, 30*time.Second, HeatRateLimitWindow(20), "short tracks use the floor")
	assert.Equal(t, 105*time.Second, HeatRateLimitWindow(100), "window outlives the track by the grace period")
	// Absurd durations are clamped instead of overflowing into a negative/floor
	// window.
	assert.Equal(t, time.Duration(heatMaxWindowSeconds)*time.Second+HeatPlayRateGrace, HeatRateLimitWindow(1e18))
}

// Pins the runtime dedupe-key formats.
func TestHeatDedupeKeyFormats(t *testing.T) {
	assert.Equal(t, "fav:u-1:t-1", FavoriteDedupeKey("u-1", "t-1"))
	assert.Equal(t, "pl:p-1:t-1", PlaylistDedupeKey("p-1", "t-1"))
	assert.Equal(t, "play:u-1:t-1:s-1", PlaySessionKey("u-1", "t-1", "s-1"))
	assert.Equal(t, "playc:u-1:t-1:s-1", PlayCompleteSessionKey("u-1", "t-1", "s-1"))
}

func TestHeatQualification(t *testing.T) {
	assert.True(t, HeatQualifiesForListened(30, 300), "30s of a long track qualifies")
	assert.True(t, HeatQualifiesForListened(12, 20), "half of a short track qualifies")
	assert.False(t, HeatQualifiesForListened(15, 300), "15s of a long track does not qualify")
	assert.False(t, HeatListenedIsComplete(50, 100))
	assert.True(t, HeatListenedIsComplete(95, 100))
	assert.False(t, HeatListenedIsComplete(30, 200), "a short partial listen is not complete")
}
