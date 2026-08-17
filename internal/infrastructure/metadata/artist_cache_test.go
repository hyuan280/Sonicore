package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestArtistDetailCache(t *testing.T) {
	c := NewArtistDetailCache()
	assert.Nil(t, c.Get("a-1"), "miss before set")

	full := &MBArtistFull{ID: "a-1", Name: "Band", Country: "GB", Tags: []MBTag{{Name: "rock"}}}
	c.Set("a-1", full)
	assert.Same(t, full, c.Get("a-1"), "cached full detail returned")

	// Nil receiver/value are safe no-ops / clean misses.
	var nilCache *ArtistDetailCache
	assert.Nil(t, nilCache.Get("a-1"))
	assert.NotPanics(t, func() { nilCache.Set("a-1", full) })
	assert.NotPanics(t, func() { c.Set("a-2", nil) })
	assert.Nil(t, c.Get("a-2"), "nil value not stored")

	// A zero-value cache (nil map) initializes lazily instead of panicking.
	var zero ArtistDetailCache
	assert.NotPanics(t, func() { zero.Set("a-3", full) })
	assert.Same(t, full, zero.Get("a-3"))
}

func TestArtistDetailCacheContext(t *testing.T) {
	c := NewArtistDetailCache()
	ctx := WithArtistDetailCache(t.Context(), c)
	assert.Same(t, c, artistDetailCacheFrom(ctx))
	assert.Nil(t, artistDetailCacheFrom(t.Context()), "no cache outside a scan")
}
