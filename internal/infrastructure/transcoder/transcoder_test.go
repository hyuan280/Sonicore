package transcoder

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseQuality(t *testing.T) {
	assert.Equal(t, QualityLossless, ParseQuality("lossless"))
	assert.Equal(t, QualityHigh, ParseQuality("high"))
	assert.Equal(t, QualityStandard, ParseQuality("standard"))
	assert.Equal(t, QualityStandard, ParseQuality(""))
	assert.Equal(t, QualityStandard, ParseQuality("ultra"), "unknown falls back to standard")
}

func TestCodecPlayable(t *testing.T) {
	assert.True(t, codecPlayable("mp3"))
	assert.True(t, codecPlayable("flac"))
	assert.True(t, codecPlayable("aac"))
	assert.True(t, codecPlayable("opus"))
	assert.True(t, codecPlayable(""), "empty codec assumed playable")
	assert.False(t, codecPlayable("alac"))
	assert.False(t, codecPlayable("ac3"))
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name     string
		bitrate  int
		codec    string
		quality  Quality
		transcode bool
	}{
		{"lossless playable no transcode", 1411000, "flac", QualityLossless, false},
		{"lossless unplayable transcodes", 100000, "alac", QualityLossless, true},
		{"high below target no transcode", 128000, "mp3", QualityHigh, false},
		{"high above target transcodes", 500000, "mp3", QualityHigh, true},
		{"standard above target transcodes", 300000, "mp3", QualityStandard, true},
		{"standard below target no transcode", 100000, "mp3", QualityStandard, false},
		{"unplayable codec always transcodes", 1000, "ac3", QualityHigh, true},
		{"unknown bitrate playable no transcode", 0, "mp3", QualityHigh, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.transcode, Decide(tt.bitrate, tt.codec, tt.quality).Transcode)
		})
	}
}

func TestResolveConfig(t *testing.T) {
	lossless := resolveConfig(QualityLossless)
	assert.Equal(t, "flac", lossless.codec)
	assert.Equal(t, ".m4a", lossless.ext)
	assert.Equal(t, "audio/mp4", lossless.contentType)
	assert.True(t, lossless.experimental)

	high := resolveConfig(QualityHigh)
	assert.Equal(t, "aac", high.codec)
	assert.Equal(t, "320k", high.bitrate)

	std := resolveConfig(QualityStandard)
	assert.Equal(t, "256k", std.bitrate)
}

func TestCacheKeyDeterministic(t *testing.T) {
	a := cacheKey("/music/song.flac", "high")
	b := cacheKey("/music/song.flac", "high")
	c := cacheKey("/music/song.flac", "lossless")

	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
	assert.NotEqual(t, cacheKey("/music/other.flac", "high"), a)
	assert.Contains(t, a, "_high")
}

func TestCachePath(t *testing.T) {
	dir := t.TempDir()
	cacheDir = dir

	p := cachePath("/music/song.flac", "high", ".m4a")
	assert.Equal(t, dir+string(filepath.Separator)+cacheKey("/music/song.flac", "high")+".m4a", p)
}

func TestCacheValid(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "song.flac")
	cached := filepath.Join(dir, "cached.m4a")

	require.NoError(t, os.WriteFile(src, []byte("x"), 0644))
	require.NoError(t, os.WriteFile(cached, []byte("y"), 0644))

	// cached older than source → invalid
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(cached, old, old))
	assert.False(t, cacheValid(cached, src))

	// cached newer than source → valid
	now := time.Now()
	require.NoError(t, os.Chtimes(cached, now, now))
	require.NoError(t, os.Chtimes(src, now.Add(-time.Hour), now.Add(-time.Hour)))
	assert.True(t, cacheValid(cached, src))

	assert.False(t, cacheValid(filepath.Join(dir, "missing"), src), "missing cache file")
	assert.False(t, cacheValid(cached, filepath.Join(dir, "missing-src")), "missing source file")
}

func TestCacheReadyWithoutInit(t *testing.T) {
	cacheDir = ""
	assert.False(t, CacheReady("/music/song.flac", QualityHigh))
}

func TestIsSeekRange(t *testing.T) {
	assert.True(t, isSeekRange("bytes=100-199"))
	assert.True(t, isSeekRange("bytes=100-"))
	assert.False(t, isSeekRange("bytes=0-99"), "start at 0 is not a seek")
	assert.False(t, isSeekRange("bytes=-100"), "suffix range is not a seek")
	assert.False(t, isSeekRange(""), "empty")
	assert.False(t, isSeekRange("items=0-5"), "wrong unit")
	assert.False(t, isSeekRange("bytes=abc-"), "non-numeric")
}

func TestLockInflightDeduplicates(t *testing.T) {
	release := lockInflight("job-1")
	require.NotNil(t, release)

	done := make(chan bool)
	go func() {
		// second lock blocks until the first releases
		release2 := lockInflight("job-1")
		release2()
		done <- true
	}()

	select {
	case <-done:
		t.Fatal("second lock should block while first is held")
	case <-time.After(100 * time.Millisecond):
	}

	release()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second lock did not acquire after release")
	}

	// after both released, a fresh lock must not block
	release3 := lockInflight("job-1")
	release3()
}
