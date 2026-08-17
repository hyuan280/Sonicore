package metadata

import (
	"context"
	"sync"
)

// ArtistDetailCache memoizes MusicBrainz artist lookups for the duration of a
// single scan, keyed by artist MBID. The full detail is cached (country,
// genre tags, ...) so every consumer of an artist detail reuses one lookup per
// artist instead of re-requesting MusicBrainz for each track that credits the
// artist. The cache is created per scan and dropped with it; non-scan paths
// (REST handlers) pass no cache and behave as before.
type ArtistDetailCache struct {
	mu sync.Mutex
	m  map[string]*MBArtistFull
}

// NewArtistDetailCache returns an empty cache.
func NewArtistDetailCache() *ArtistDetailCache {
	return &ArtistDetailCache{m: make(map[string]*MBArtistFull)}
}

// Get returns the cached detail for an artist MBID, or nil on a miss. A nil
// receiver (no cache in the context) is a clean miss.
func (c *ArtistDetailCache) Get(mbid string) *MBArtistFull {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[mbid]
}

// Set records the detail for an artist MBID. A nil receiver or value is a
// no-op; a zero-value cache (nil map) is initialized lazily.
func (c *ArtistDetailCache) Set(mbid string, full *MBArtistFull) {
	if c == nil || full == nil {
		return
	}
	c.mu.Lock()
	if c.m == nil {
		c.m = make(map[string]*MBArtistFull)
	}
	c.m[mbid] = full
	c.mu.Unlock()
}

type artistDetailCacheKey struct{}

// WithArtistDetailCache attaches a per-scan artist cache to the context. The
// scanner wires one fresh cache per ScanLibrary call so cached artist details
// never leak across scans.
func WithArtistDetailCache(ctx context.Context, c *ArtistDetailCache) context.Context {
	return context.WithValue(ctx, artistDetailCacheKey{}, c)
}

// artistDetailCacheFrom returns the cache carried by the context, or nil when
// the caller is not inside a scan (no caching).
func artistDetailCacheFrom(ctx context.Context) *ArtistDetailCache {
	c, _ := ctx.Value(artistDetailCacheKey{}).(*ArtistDetailCache)
	return c
}
