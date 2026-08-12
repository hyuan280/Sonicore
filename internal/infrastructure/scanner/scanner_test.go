package scanner

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTitleCase(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello", "Hello"},
		{"Hello", "Hello"},
		{"", ""},
		{"a", "A"},
		{"héllo", "Héllo"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, titleCase(tt.in))
		})
	}
}

func TestIsYear(t *testing.T) {
	assert.True(t, isYear("1984"))
	assert.True(t, isYear("0000"))
	assert.False(t, isYear("84"))
	assert.False(t, isYear("19845"))
	assert.False(t, isYear("19a4"))
	assert.False(t, isYear(""))
}

func TestSplitByPunct(t *testing.T) {
	assert.Equal(t, []string{"Song", "Title", "Version"}, splitByPunct("Song (Title) - Version"))
	assert.Equal(t, []string{"a", "b", "c", "d"}, splitByPunct("a.b_c/d"))
	assert.Equal(t, []string{"Solo"}, splitByPunct("'Solo'"))
	assert.Empty(t, splitByPunct("---"))
	assert.Empty(t, splitByPunct(""))
}

func TestExtractFromPath(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		stem     string
		title    string
		artist   string
		album    string
		filePath string
		want     string
	}{
		{
			name:     "tags matched and removed",
			dir:      "/music/My Band/Album 2020",
			stem:     "Song (Live in Paris)",
			title:    "Song",
			artist:   "My Band",
			album:    "Album",
			filePath: "/music/My Band/Album 2020/Song (Live in Paris).flac",
			want:     "Music, Live, In, Paris",
		},
		{
			name:     "year dropped",
			dir:      "/music",
			stem:     "Track 1999",
			title:    "Track",
			artist:   "Artist",
			album:    "Album",
			filePath: "/music/Track 1999.mp3",
			want:     "Music",
		},
		{
			name:     "title tokens all blacklisted, dir word remains",
			dir:      "/music",
			stem:     "Same",
			title:    "Same",
			artist:   "",
			album:    "",
			filePath: "/music/Same.mp3",
			want:     "Music",
		},
		{
			name:     "album words blacklisted",
			dir:      "/music",
			stem:     "Intro (Greatest Hits)",
			title:    "Intro",
			artist:   "",
			album:    "Greatest Hits",
			filePath: "/music/Intro (Greatest Hits).flac",
			want:     "Music", // title/album/artist words dropped, dir word survives
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFromPath(tt.dir, tt.stem, tt.title, tt.artist, tt.album, tt.filePath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0644))

	h1, err := hashFile(path)
	require.NoError(t, err)
	assert.Len(t, h1, 64, "sha256 hex")

	h2, err := hashFile(path)
	require.NoError(t, err)
	assert.Equal(t, h1, h2, "same content same hash")

	require.NoError(t, os.WriteFile(path, []byte("changed"), 0644))
	h3, err := hashFile(path)
	require.NoError(t, err)
	assert.NotEqual(t, h1, h3, "different content different hash")
}

func TestHashFileLargeContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	data := make([]byte, 1<<20)
	_, err := rand.Read(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))

	h, err := hashFile(path)
	require.NoError(t, err)
	assert.Len(t, h, 64)
}

func TestHashFileMissing(t *testing.T) {
	_, err := hashFile("/nonexistent/file.mp3")
	require.Error(t, err)
}

func TestTimePtr(t *testing.T) {
	now := time.Now()
	p := timePtr(now)
	require.NotNil(t, p)
	assert.Equal(t, now, *p)
}
