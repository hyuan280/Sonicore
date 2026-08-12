package domain

import (
	"database/sql/driver"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ driver.Valuer = ImageVariants{}
var _ driver.Valuer = (*ImageVariants)(nil)

func TestNewID(t *testing.T) {
	id := NewID()
	assert.Len(t, id, 26, "ID must be 26 chars")

	for _, c := range id {
		isDigit := c >= '0' && c <= '9'
		isHex := (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		assert.True(t, isDigit || isHex, "ID contains invalid char %q", c)
	}
}

func TestNewIDUniqueness(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := NewID()
		_, exists := seen[id]
		assert.False(t, exists, "duplicate ID generated: %s", id)
		seen[id] = struct{}{}
	}
}

func TestNewIDTimestampPrefix(t *testing.T) {
	before := time.Now().UnixMilli()
	id := NewID()
	after := time.Now().UnixMilli()

	prefix, err := strconv.ParseInt(id[:13], 10, 64)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, prefix, before)
	assert.LessOrEqual(t, prefix, after)
}

func TestNewIDOrderedAcrossMilliseconds(t *testing.T) {
	prev := NewID()
	time.Sleep(5 * time.Millisecond)
	cur := NewID()
	assert.Greater(t, cur, prev, "IDs from different milliseconds should be ordered")
}

func TestImageVariantsValue(t *testing.T) {
	variants := ImageVariants{
		{Path: "/a.png", Width: 100, Height: 100, Size: 1024},
		{Path: "/b.png", Width: 300, Height: 300, Size: 4096},
	}

	data, err := variants.Value()
	require.NoError(t, err)

	var decoded []ImageVariant
	require.NoError(t, json.Unmarshal(data.([]byte), &decoded))
	assert.Equal(t, []ImageVariant(variants), decoded)
}

func TestImageVariantsValueEmpty(t *testing.T) {
	variants := ImageVariants{}
	data, err := variants.Value()
	require.NoError(t, err)
	assert.JSONEq(t, "[]", string(data.([]byte)))
}

func TestImageVariantsScan(t *testing.T) {
	src := []byte(`[{"path":"/a.png","width":100,"height":100,"size":1024}]`)

	var variants ImageVariants
	require.NoError(t, variants.Scan(src))
	require.Len(t, variants, 1)
	assert.Equal(t, "/a.png", variants[0].Path)
	assert.Equal(t, 100, variants[0].Width)
	assert.Equal(t, 100, variants[0].Height)
	assert.Equal(t, int64(1024), variants[0].Size)
}

func TestImageVariantsScanNil(t *testing.T) {
	var variants ImageVariants
	require.NoError(t, variants.Scan(nil))
	assert.Empty(t, variants)
}

func TestImageVariantsScanInvalidJSON(t *testing.T) {
	var variants ImageVariants
	require.Error(t, variants.Scan([]byte("not-json")))
}

func TestImageVariantsScanRoundTrip(t *testing.T) {
	original := ImageVariants{
		{Path: "/x.png", Width: 200, Height: 200, Size: 2048},
	}
	data, err := original.Value()
	require.NoError(t, err)

	var decoded ImageVariants
	require.NoError(t, decoded.Scan(data))
	assert.Equal(t, original, decoded)
}
