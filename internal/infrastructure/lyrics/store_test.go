package lyrics

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriorityBit(t *testing.T) {
	tests := []struct {
		priority int
		want     int
	}{
		{PriorityEmbedded, 1},
		{PrioritySidecar, 2},
		{PriorityNetwork, 4},
		{PriorityUser, 8},
	}

	for _, tt := range tests {
		t.Run(string(rune('0'+tt.priority)), func(t *testing.T) {
			assert.Equal(t, tt.want, PriorityBit(tt.priority))
		})
	}
}

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"lrc with timestamp", "[00:12.34]Line one\n[00:20.00]Line two", "lrc"},
		{"lrc with no leading zeros", "[1:02.00]Line", "lrc"},
		{"lrc with metadata tags", "[ti:Some Song]\n[00:05.00]Hello", "lrc"},
		{"lrc detected on any line", "Just a line\n[00:12.34]not lrc", "lrc"},
		{"empty content", "", "txt"},
		{"blank lines only", "\n\n  \n", "txt"},
		{"single bracket without time", "[not-a-time]line", "txt"},
		{"bracket too long", "[00:12.3456789 extra]line", "txt"},
		{"no brackets", "Just plain lyrics", "txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, detectFormat(tt.content))
		})
	}
}

func TestStoreSaveGetRoundTrip(t *testing.T) {
	store := NewStore(t.TempDir())
	content := "[00:01.00]Hello world\n[00:05.00]Second line"

	require.NoError(t, store.Save("lib-1", "track-1", PrioritySidecar, content))

	got, priority, format, actualMask, err := store.Get("lib-1", "track-1", PriorityBit(PrioritySidecar))
	require.NoError(t, err)
	assert.Equal(t, content, got)
	assert.Equal(t, PrioritySidecar, priority)
	assert.Equal(t, "lrc", format)
	assert.Equal(t, PriorityBit(PrioritySidecar), actualMask)
}

func TestStoreSaveOverwritesSamePriority(t *testing.T) {
	store := NewStore(t.TempDir())

	require.NoError(t, store.Save("lib-1", "track-1", PriorityUser, "first"))
	require.NoError(t, store.Save("lib-1", "track-1", PriorityUser, "second"))

	got, _, _, _, err := store.Get("lib-1", "track-1", PriorityBit(PriorityUser))
	require.NoError(t, err)
	assert.Equal(t, "second", got)
}

func TestStoreGetPriorityOrder(t *testing.T) {
	store := NewStore(t.TempDir())

	require.NoError(t, store.Save("lib-1", "track-1", PriorityEmbedded, "embedded"))
	require.NoError(t, store.Save("lib-1", "track-1", PriorityNetwork, "network"))
	require.NoError(t, store.Save("lib-1", "track-1", PriorityUser, "user"))

	mask := PriorityBit(PriorityEmbedded) | PriorityBit(PriorityNetwork) | PriorityBit(PriorityUser)
	got, priority, _, actualMask, err := store.Get("lib-1", "track-1", mask)
	require.NoError(t, err)
	assert.Equal(t, "user", got)
	assert.Equal(t, PriorityUser, priority)
	assert.Equal(t, mask, actualMask, "all present priorities should be reported")
}

func TestStoreGetHonorsMask(t *testing.T) {
	store := NewStore(t.TempDir())
	require.NoError(t, store.Save("lib-1", "track-1", PriorityUser, "user"))
	require.NoError(t, store.Save("lib-1", "track-1", PriorityNetwork, "network"))

	got, priority, _, _, err := store.Get("lib-1", "track-1", PriorityBit(PriorityNetwork))
	require.NoError(t, err)
	assert.Equal(t, "network", got)
	assert.Equal(t, PriorityNetwork, priority)
}

func TestStoreGetReportsMissingMaskBits(t *testing.T) {
	store := NewStore(t.TempDir())
	require.NoError(t, store.Save("lib-1", "track-1", PriorityUser, "user"))

	_, _, _, actualMask, err := store.Get("lib-1", "track-1", PriorityBit(PriorityUser)|PriorityBit(PrioritySidecar))
	require.NoError(t, err)
	assert.Equal(t, PriorityBit(PriorityUser), actualMask, "mask bit without file should be reported missing")
}

func TestStoreGetNotFound(t *testing.T) {
	store := NewStore(t.TempDir())

	_, _, _, _, err := store.Get("lib-1", "missing", PriorityBit(PriorityUser))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no lyrics found")
}

func TestStoreGetAcrossLibraries(t *testing.T) {
	store := NewStore(t.TempDir())
	require.NoError(t, store.Save("lib-1", "track-1", PriorityUser, "lib1 content"))
	require.NoError(t, store.Save("lib-2", "track-1", PriorityUser, "lib2 content"))

	got, _, _, _, err := store.Get("lib-2", "track-1", PriorityBit(PriorityUser))
	require.NoError(t, err)
	assert.Equal(t, "lib2 content", got)
}

func TestStoreDelete(t *testing.T) {
	store := NewStore(t.TempDir())
	require.NoError(t, store.Save("lib-1", "track-1", PriorityUser, "content"))

	require.NoError(t, store.Delete("lib-1", "track-1", PriorityUser))
	_, _, _, _, err := store.Get("lib-1", "track-1", PriorityBit(PriorityUser))
	require.Error(t, err)
}

func TestStoreDeleteMissingFile(t *testing.T) {
	store := NewStore(t.TempDir())
	require.NoError(t, store.Delete("lib-1", "track-1", PriorityUser))
}

func TestStoreDeleteAll(t *testing.T) {
	store := NewStore(t.TempDir())
	require.NoError(t, store.Save("lib-1", "track-1", PriorityUser, "user"))
	require.NoError(t, store.Save("lib-1", "track-1", PriorityNetwork, "network"))
	require.NoError(t, store.Save("lib-1", "track-2", PriorityUser, "other"))

	require.NoError(t, store.DeleteAll("lib-1", "track-1"))

	_, _, _, _, err := store.Get("lib-1", "track-1", PriorityBit(PriorityUser)|PriorityBit(PriorityNetwork))
	require.Error(t, err)

	got, _, _, _, err := store.Get("lib-1", "track-2", PriorityBit(PriorityUser))
	require.NoError(t, err)
	assert.Equal(t, "other", got)
}

func TestStoreFilesArePidIsolated(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	require.NoError(t, store.Save("lib-1", "track-1", PriorityUser, "user txt"))
	require.NoError(t, store.Save("lib-1", "track-1", PriorityUser, "[00:01.00]user lrc"))

	files, err := os.ReadDir(filepath.Join(dir, "lib-1"))
	require.NoError(t, err)
	assert.Len(t, files, 1, "same priority should leave only one file")
	assert.Equal(t, "track-1_p3.lrc", files[0].Name())
}
