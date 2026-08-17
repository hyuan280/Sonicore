package metadata

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectImageType(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}
	jpg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"png magic bytes", png, "png"},
		{"jpg defaults", jpg, "jpg"},
		{"short data defaults to jpg", []byte{0x89, 0x50}, "jpg"},
		{"empty defaults to jpg", nil, "jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, detectImageType(tt.data))
		})
	}
}

func TestSweepOrphanCovers(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	imagesDir := t.TempDir()
	orphanPath := filepath.Join(imagesDir, "lib-1", "track_gone-1.jpg")
	require.NoError(t, os.MkdirAll(filepath.Dir(orphanPath), 0755))
	require.NoError(t, os.WriteFile(orphanPath, []byte("jpeg"), 0644))
	variantPath := filepath.Join(imagesDir, "lib-1", "track_gone-1_64.jpg")
	require.NoError(t, os.WriteFile(variantPath, []byte("jpeg"), 0644))

	now := time.Date(2024, 10, 1, 20, 0, 0, 0, time.UTC)
	findQuery := regexp.QuoteMeta(`SELECT id, library_id, owner_type, owner_id, source, path,
		 format, width, height, size, hash, variants, created_at, updated_at
		 FROM images
		 WHERE (owner_type = 'track' AND NOT EXISTS (SELECT 1 FROM tracks t WHERE t.id = images.owner_id))
		    OR (owner_type = 'album' AND NOT EXISTS (SELECT 1 FROM albums a WHERE a.id = images.owner_id))
		    OR (owner_type = 'artist' AND NOT EXISTS (SELECT 1 FROM artists ar WHERE ar.id = images.owner_id))`)
	mock.ExpectQuery(findQuery).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "library_id", "owner_type", "owner_id", "source", "path",
			"format", "width", "height", "size", "hash", "variants", "created_at", "updated_at",
		}).AddRow("img-1", "lib-1", "track", "gone-1", "embed", orphanPath, "jpg", 600, 600, 1234, "h1",
			`[{"path":"`+variantPath+`","width":64,"height":64,"size":100}]`, now, now))
	// Files are removed before the row (so a failed removal stays retryable);
	// both paths count as unreferenced, then the row is deleted.
	countQ := regexp.QuoteMeta(`SELECT COUNT(*) FROM images WHERE path = $1 AND id != $2`)
	mock.ExpectQuery(countQ).WithArgs(orphanPath, "img-1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(countQ).WithArgs(variantPath, "img-1").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM images WHERE id = $1`)).
		WithArgs("img-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	m := &CoverManager{imagesDir: imagesDir, images: repository.NewImageRepo(db)}
	require.NoError(t, m.SweepOrphanCovers(context.Background()))

	_, err = os.Stat(orphanPath)
	assert.True(t, os.IsNotExist(err), "orphan original removed")
	_, err = os.Stat(variantPath)
	assert.True(t, os.IsNotExist(err), "orphan variant removed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSweepOrphanCoversKeepsSharedFile(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	imagesDir := t.TempDir()
	// An orphaned album row referencing a still-live track's cover file.
	sharedPath := filepath.Join(imagesDir, "lib-1", "track_live-1.jpg")
	require.NoError(t, os.MkdirAll(filepath.Dir(sharedPath), 0755))
	require.NoError(t, os.WriteFile(sharedPath, []byte("jpeg"), 0644))

	now := time.Date(2024, 10, 1, 20, 0, 0, 0, time.UTC)
	findQuery := regexp.QuoteMeta(`SELECT id, library_id, owner_type, owner_id, source, path,
		 format, width, height, size, hash, variants, created_at, updated_at
		 FROM images
		 WHERE (owner_type = 'track' AND NOT EXISTS (SELECT 1 FROM tracks t WHERE t.id = images.owner_id))
		    OR (owner_type = 'album' AND NOT EXISTS (SELECT 1 FROM albums a WHERE a.id = images.owner_id))
		    OR (owner_type = 'artist' AND NOT EXISTS (SELECT 1 FROM artists ar WHERE ar.id = images.owner_id))`)
	mock.ExpectQuery(findQuery).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "library_id", "owner_type", "owner_id", "source", "path",
			"format", "width", "height", "size", "hash", "variants", "created_at", "updated_at",
		}).AddRow("img-alb", nil, "album", "gone-alb", "backfill", sharedPath, "jpg", 600, 600, 1234, "h1", "[]", now, now))
	// The path is still referenced by the live track row → file must survive,
	// but the shared reference is not a failure, so the orphan row is deleted.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM images WHERE path = $1 AND id != $2`)).
		WithArgs(sharedPath, "img-alb").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM images WHERE id = $1`)).
		WithArgs("img-alb").
		WillReturnResult(sqlmock.NewResult(0, 1))

	m := &CoverManager{imagesDir: imagesDir, images: repository.NewImageRepo(db)}
	require.NoError(t, m.SweepOrphanCovers(context.Background()))

	_, err = os.Stat(sharedPath)
	assert.NoError(t, err, "shared track cover kept")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSweepOrphanCoversKeepsRowWhenFileRemovalFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	imagesDir := t.TempDir()
	// A directory named like the file makes os.Remove fail on non-empty dirs;
	// simpler: point the row at a file that does not exist — removal is
	// idempotent there, so to simulate a real failure we make the parent
	// path a non-empty directory.
	stuckPath := filepath.Join(imagesDir, "lib-1", "track_gone-2.jpg")
	require.NoError(t, os.MkdirAll(stuckPath+"/sub", 0755))

	now := time.Date(2024, 10, 1, 20, 0, 0, 0, time.UTC)
	findQuery := regexp.QuoteMeta(`SELECT id, library_id, owner_type, owner_id, source, path,
		 format, width, height, size, hash, variants, created_at, updated_at
		 FROM images
		 WHERE (owner_type = 'track' AND NOT EXISTS (SELECT 1 FROM tracks t WHERE t.id = images.owner_id))
		    OR (owner_type = 'album' AND NOT EXISTS (SELECT 1 FROM albums a WHERE a.id = images.owner_id))
		    OR (owner_type = 'artist' AND NOT EXISTS (SELECT 1 FROM artists ar WHERE ar.id = images.owner_id))`)
	mock.ExpectQuery(findQuery).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "library_id", "owner_type", "owner_id", "source", "path",
			"format", "width", "height", "size", "hash", "variants", "created_at", "updated_at",
		}).AddRow("img-2", "lib-1", "track", "gone-2", "embed", stuckPath, "jpg", 600, 600, 1234, "h1", "[]", now, now))
	// Unreferenced, but os.Remove fails (non-empty directory) → the row must
	// be retained so the next sweep retries: no DELETE is expected.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM images WHERE path = $1 AND id != $2`)).
		WithArgs(stuckPath, "img-2").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	m := &CoverManager{imagesDir: imagesDir, images: repository.NewImageRepo(db)}
	require.NoError(t, m.SweepOrphanCovers(context.Background()))

	_, err = os.Stat(stuckPath)
	assert.NoError(t, err, "file that could not be removed survives")
	require.NoError(t, mock.ExpectationsWereMet(), "no row delete happens so the leak stays retryable")
}

func TestCoverPath(t *testing.T) {
	assert.Equal(t,
		filepath.Join("/img", "lib-1", "album_alb-1.jpg"),
		CoverPath("/img", "lib-1", "album", "alb-1", "jpg"))
}

func TestCoverPathWithSuffix(t *testing.T) {
	assert.Equal(t,
		filepath.Join("/img", "lib-1", "track_t-1_64.jpg"),
		CoverPathWithSuffix("/img", "lib-1", "track", "t-1", "_64", "jpg"))
}

func TestCoverPathWithSuffixEmptySuffix(t *testing.T) {
	assert.Equal(t,
		filepath.Join("/img", "lib-1", "track_t-1.jpg"),
		CoverPathWithSuffix("/img", "lib-1", "track", "t-1", "", "jpg"))
}

func makePNG(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestResizeToThumbnailSmallImageNoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.jpg")

	// 50x50 smaller than max 64 → no thumbnail written; the serving chain
	// falls back to the original file.
	require.NoError(t, ResizeToThumbnail(makePNG(t, 50), path, 64))
	_, err := os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "small image should not produce a thumbnail")
}

func TestResizeToThumbnailLargeImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.jpg")

	require.NoError(t, ResizeToThumbnail(makePNG(t, 800), path, 64))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	decoded, format, err := image.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, "jpeg", format)
	assert.Equal(t, 64, decoded.Bounds().Dx())
	assert.Equal(t, 64, decoded.Bounds().Dy())
}

func TestResizeToThumbnailTallImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.jpg")

	img := image.NewRGBA(image.Rect(0, 0, 400, 800))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	require.NoError(t, ResizeToThumbnail(buf.Bytes(), path, 64))

	decoded, _, err := image.Decode(bytes.NewReader(mustRead(t, path)))
	require.NoError(t, err)
	assert.Equal(t, 32, decoded.Bounds().Dx(), "width scales with the taller dimension")
	assert.Equal(t, 64, decoded.Bounds().Dy())
}

func TestResizeToThumbnailInvalidData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.jpg")
	require.Error(t, ResizeToThumbnail([]byte("not-an-image"), path, 64))
	_, err := os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "invalid data must not create a file")
}

func TestCoverExtractorSave(t *testing.T) {
	dir := t.TempDir()
	ce := NewCoverExtractor(dir)
	data := makePNG(t, 300)

	path, err := ce.Save("lib-1", "album", "alb-1", data, "png")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "lib-1", "album_alb-1.png"), path)

	// original written
	_, err = os.Stat(filepath.Join(dir, "lib-1", "album_alb-1.png"))
	require.NoError(t, err)

	// default thumbnails 64 + 256
	for _, size := range []int{64, 256} {
		_, err = os.Stat(filepath.Join(dir, "lib-1", "album_alb-1_"+strconv.Itoa(size)+".jpg"))
		require.NoError(t, err, "thumbnail %d should exist", size)
	}
}

func TestCoverExtractorSaveCustomSizes(t *testing.T) {
	dir := t.TempDir()
	ce := NewCoverExtractor(dir)

	_, err := ce.Save("lib-1", "artist", "a-1", makePNG(t, 128), "jpg", 48)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "lib-1", "artist_a-1_48.jpg"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "lib-1", "artist_a-1_64.jpg"))
	assert.ErrorIs(t, err, os.ErrNotExist, "only requested size should be created")
}

func TestCoverExtractorExtractFromFileNoFFmpeg(t *testing.T) {
	ce := NewCoverExtractor(t.TempDir())
	_, _, err := ce.ExtractFromFile(context.Background(), "/nonexistent/audio.mp3")
	require.Error(t, err)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func TestSummarizeFFmpegError(t *testing.T) {
	full := "ffmpeg version 5.1.9\n  configuration: --prefix=/usr\nInput #0, flac, from '/x.flac':\n  Duration: 00:00:30.00\nOutput #0, image2, to 'pipe:1':\nOutput file #0 does not contain any stream"
	got := summarizeFFmpegError(full)
	// The full stderr is kept verbatim (root-cause line must never be lost).
	assert.Contains(t, got, "ffmpeg version", "banner kept in full")
	assert.Contains(t, got, "Output file #0 does not contain any stream", "cause kept in full")
	assert.Equal(t, strings.TrimSpace(full), got, "whitespace-trimmed passthrough")

	assert.Equal(t, "ffmpeg failed", summarizeFFmpegError("  \n\n"))
}

func TestFetchImageOK(t *testing.T) {
	pngData := makePNG(t, 300)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngData)
	}))
	t.Cleanup(srv.Close)

	data, err := fetchImageUnchecked(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, pngData, data)
}

func TestFetchImageErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/notfound":
			w.WriteHeader(http.StatusNotFound)
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>not an image</html>"))
		default:
			w.Write([]byte("ok"))
		}
	}))
	t.Cleanup(srv.Close)

	_, err := fetchImageUnchecked(context.Background(), srv.URL+"/notfound")
	require.ErrorContains(t, err, "status 404")

	_, err = fetchImageUnchecked(context.Background(), srv.URL+"/html")
	require.ErrorContains(t, err, "not a decodable image")

	_, err = fetchImageUnchecked(context.Background(), "http://127.0.0.1:1/x")
	require.Error(t, err, "connection refused")
}

func TestFetchImageTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte{0xFF, 0xD8}, maxNetworkCoverBytes/2+100))
	}))
	t.Cleanup(srv.Close)

	_, err := fetchImageUnchecked(context.Background(), srv.URL)
	require.ErrorContains(t, err, "exceeds")
}

func TestSafeCoverURL(t *testing.T) {
	assert.True(t, safeCoverURL("https://p1.music.126.net/cover.jpg"))
	assert.True(t, safeCoverURL("http://coverartarchive.org/x/y.jpg"))
	assert.False(t, safeCoverURL("ftp://example.com/x"), "non-http scheme")
	assert.False(t, safeCoverURL("http://127.0.0.1/x"), "loopback")
	assert.False(t, safeCoverURL("http://169.254.169.254/latest/meta-data/"), "cloud metadata")
	assert.False(t, safeCoverURL("http://10.0.0.5/x"), "private")
	assert.False(t, safeCoverURL("http://192.168.3.1/x"), "private")
	assert.False(t, safeCoverURL("http://localhost/x"), "localhost")
	assert.False(t, safeCoverURL("not-a-url"), "unparsable")

	// fetchImage enforces the guard before any network I/O.
	_, err := fetchImage(context.Background(), "http://127.0.0.1:1/x")
	require.ErrorContains(t, err, "rejected URL")
}
