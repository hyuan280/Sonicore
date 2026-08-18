package download

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/domain"
)

func TestDetectExtFromURL(t *testing.T) {
	assert.Equal(t, "flac", detectExt("", "https://example.com/song.flac"))
	assert.Equal(t, "mp3", detectExt("", "https://example.com/x.mp3?token=abc"), "query string is stripped")
	assert.Equal(t, "m4a", detectExt("", "https://example.com/x.m4a#frag"), "fragment is stripped")
	assert.Equal(t, "M4A", detectExt("", "https://example.com/x.M4A"), "extension is returned as-is")
}

func TestDetectExtFromContentType(t *testing.T) {
	tests := []struct {
		contentType string
		want        string
	}{
		{"audio/mpeg", "mp3"},
		{"audio/flac", "flac"},
		{"audio/ogg", "ogg"},
		{"audio/mp4", "m4a"},
		{"audio/wav", "wav"},
		{"audio/x-wav", "wav"},
		{"application/octet-stream", "bin"},
		{"", "bin"},
	}
	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			assert.Equal(t, tt.want, detectExt(tt.contentType, "https://example.com/download"))
		})
	}
}

func TestExtractFilename(t *testing.T) {
	tests := []struct {
		name string
		url  string
		ext  string
		want string
	}{
		{"plain name", "https://e.com/song.mp3", "mp3", "song.mp3"},
		{"query string stripped", "https://e.com/song.mp3?token=1&x=2", "mp3", "song.mp3"},
		{"no extension gets appended", "https://e.com/abc123", "flac", "abc123.flac"},
		{"trailing slash yields only extension", "https://e.com/", "bin", ".bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractFilename(tt.url, tt.ext))
		})
	}
}

func TestDirectSourceMatch(t *testing.T) {
	src := NewDirectSource()
	assert.True(t, src.Match("http://example.com/song.mp3"))
	assert.True(t, src.Match("https://example.com/song.mp3"))
	assert.False(t, src.Match("ftp://example.com/song.mp3"))
	assert.False(t, src.Match("/local/path/song.mp3"))
	assert.False(t, src.Match(""))
	assert.Equal(t, "direct", src.Name())
}

func TestDirectSourceFetchDownloadsFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.Header.Get("User-Agent"), "Sonicore")
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("fake-mp3-bytes"))
	}))
	defer srv.Close()

	src := NewDirectSource()
	job := &domain.DownloadJob{
		URL:        srv.URL + "/track-123",
		TargetPath: filepath.Join(t.TempDir(), "out.mp3"),
	}

	require.NoError(t, src.Fetch(httptest.NewRequest(http.MethodGet, "/", nil).Context(), job))

	assert.Equal(t, float64(100), job.Progress)
	data, err := os.ReadFile(job.TargetPath)
	require.NoError(t, err)
	assert.Equal(t, "fake-mp3-bytes", string(data))
}

func TestDirectSourceFetchDerivesFilenameFromURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	src := NewDirectSource()
	job := &domain.DownloadJob{
		URL:        srv.URL + "/track-123?token=x",
		TargetPath: filepath.Join(t.TempDir(), "downloads") + "/", // trailing slash = directory
	}

	require.NoError(t, src.Fetch(httptest.NewRequest(http.MethodGet, "/", nil).Context(), job))

	assert.Equal(t, "track-123.mp3", filepath.Base(job.TargetPath), "filename from URL, ext from content-type")
	data, err := os.ReadFile(job.TargetPath)
	require.NoError(t, err)
	assert.Equal(t, "data", string(data))
}

func TestDirectSourceFetchNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	src := NewDirectSource()
	job := &domain.DownloadJob{URL: srv.URL + "/x", TargetPath: filepath.Join(t.TempDir(), "out")}

	err := src.Fetch(httptest.NewRequest(http.MethodGet, "/", nil).Context(), job)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 403")
}

func TestDirectSourceResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodHead, r.Method)
		w.Header().Set("Content-Type", "audio/flac")
		w.Header().Set("Content-Length", "12345")
	}))
	defer srv.Close()

	src := NewDirectSource()
	info, err := src.Resolve(httptest.NewRequest(http.MethodGet, "/", nil).Context(), srv.URL+"/a.flac")
	require.NoError(t, err)
	require.Len(t, info.Formats, 1)
	assert.Equal(t, "flac", info.Formats[0].Extension)
	assert.Equal(t, int64(12345), info.Formats[0].FileSize)
}

func TestDirectSourceResolveQueryURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
	}))
	defer srv.Close()

	src := NewDirectSource()
	info, err := src.Resolve(httptest.NewRequest(http.MethodGet, "/", nil).Context(), srv.URL+"/song.mp3?token=sig&expires=1")
	require.NoError(t, err)
	require.Len(t, info.Formats, 1)
	assert.Equal(t, "mp3", info.Formats[0].Extension, "query string must not leak into extension")
}
