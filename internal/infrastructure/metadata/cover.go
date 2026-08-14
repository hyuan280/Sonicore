package metadata

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/image/draw"
)

type CoverExtractor struct {
	imagesDir string
}

func NewCoverExtractor(imagesDir string) *CoverExtractor {
	return &CoverExtractor{imagesDir: imagesDir}
}

// extractCoverTimeout bounds a single ffmpeg extraction so a hung process
// cannot block the shared extraction lock indefinitely.
const extractCoverTimeout = 30 * time.Second

// ExtractFromFile pulls the embedded cover bytes from an audio file. The
// caller's context is honored (with a hard timeout) so a disconnected
// client aborts the ffmpeg subprocess instead of holding the extraction
// lock for the full duration.
func (ce *CoverExtractor) ExtractFromFile(ctx context.Context, audioPath string) ([]byte, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, extractCoverTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-i", audioPath,
		"-an",
		"-vcodec", "copy",
		"-f", "image2",
		"pipe:1",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("ffmpeg extract cover: %w\n%s", err, stderr.String())
	}

	data := stdout.Bytes()
	if len(data) == 0 {
		return nil, "", fmt.Errorf("no cover art found")
	}

	contentType := detectImageType(data)
	return data, contentType, nil
}

func (ce *CoverExtractor) Save(libraryID, ownerType, ownerID string, data []byte, ext string, sizes ...int) (string, error) {
	dir := filepath.Join(ce.imagesDir, libraryID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	filename := fmt.Sprintf("%s_%s.%s", ownerType, ownerID, ext)
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	if len(sizes) == 0 {
		sizes = []int{64, 256}
	}
	for _, size := range sizes {
		thumbPath := filepath.Join(dir, fmt.Sprintf("%s_%s_%d.jpg", ownerType, ownerID, size))
		if err := ResizeToThumbnail(data, thumbPath, size); err != nil {
			log.Printf("[cover] thumbnail error %s: %v", thumbPath, err)
		}
	}
	return path, nil
}

func detectImageType(data []byte) string {
	if len(data) < 8 {
		return "jpg"
	}
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "png"
	}
	return "jpg"
}

// ResizeToThumbnail scales data to fit maxSize and writes it as JPEG.
// A source already at or below the target size produces no file and returns
// nil (the serving chain falls back to larger sizes / the original). Any
// failure (decode, create, encode) returns an error and removes a partial
// output file so callers never treat partial bytes as a valid thumbnail.
func ResizeToThumbnail(data []byte, outputPath string, maxSize int) error {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("image.Decode: %w", err)
	}

	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	// A source smaller than the target is left untouched: no thumbnail file
	// is produced. Cover requests fall back to larger sizes / the original
	// via the serving chain, so the missing file is not an error state.
	if w <= maxSize && h <= maxSize {
		return nil
	}

	scale := float64(maxSize) / float64(w)
	if h > w {
		scale = float64(maxSize) / float64(h)
	}

	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create thumbnail: %w", err)
	}
	defer out.Close()
	if err := jpeg.Encode(out, dst, &jpeg.Options{Quality: 85}); err != nil {
		// Close before removing so the partial file can be unlinked even on
		// platforms that refuse to delete open files.
		out.Close()
		if rerr := os.Remove(outputPath); rerr != nil && !os.IsNotExist(rerr) {
			log.Printf("[cover] remove partial thumbnail %s: %v", outputPath, rerr)
		}
		return fmt.Errorf("encode thumbnail: %w", err)
	}
	return nil
}

func (ce *CoverExtractor) ImagesDir() string {
	return ce.imagesDir
}

func CoverPath(imagesDir, libraryID, ownerType, ownerID, ext string) string {
	return CoverPathWithSuffix(imagesDir, libraryID, ownerType, ownerID, "", ext)
}

func CoverPathWithSuffix(imagesDir, libraryID, ownerType, ownerID, suffix, ext string) string {
	name := fmt.Sprintf("%s_%s", ownerType, ownerID)
	if suffix != "" {
		name += suffix
	}
	return filepath.Join(imagesDir, libraryID, name+"."+ext)
}

// RemoveAlbumCover deletes every image file an album may own (main cover and
// resized thumbnails). Missing files are ignored.
func RemoveAlbumCover(imagesDir, albumID string) {
	for _, suffix := range []string{"", "_64", "_256"} {
		p := CoverPathWithSuffix(imagesDir, "album", "album", albumID, suffix, "jpg")
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			log.Printf("[cover] remove album cover error: %v", err)
		}
	}
}

// CoverFileExists reports whether a cover file is present on disk.
// CoverFileExists reports whether a cover file is present on disk. Only a
// missing file counts as absent; genuine filesystem errors (permissions,
// I/O) are logged and treated as present (fail-safe), so they are not
// mistaken for a deleted cover and do not trigger needless re-extraction.
func CoverFileExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	log.Printf("[cover] stat error %s: %v", path, err)
	return true
}
