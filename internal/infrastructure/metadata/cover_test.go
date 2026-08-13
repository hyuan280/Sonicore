package metadata

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"

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
	ResizeToThumbnail(makePNG(t, 50), path, 64)
	_, err := os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "small image should not produce a thumbnail")
}

func TestResizeToThumbnailLargeImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.jpg")

	ResizeToThumbnail(makePNG(t, 800), path, 64)

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

	ResizeToThumbnail(buf.Bytes(), path, 64)

	decoded, _, err := image.Decode(bytes.NewReader(mustRead(t, path)))
	require.NoError(t, err)
	assert.Equal(t, 32, decoded.Bounds().Dx(), "width scales with the taller dimension")
	assert.Equal(t, 64, decoded.Bounds().Dy())
}

func TestResizeToThumbnailInvalidData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thumb.jpg")
	ResizeToThumbnail([]byte("not-an-image"), path, 64)
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
	_, _, err := ce.ExtractFromFile("/nonexistent/audio.mp3")
	require.Error(t, err)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
