package metadata

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"

	"github.com/sonicore/server/internal/infrastructure/logger"
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
		// The full stderr is verbose (banner, stream dump, per-packet noise),
		// so it is logged at debug level only; the caller-facing error keeps a
		// one-line root-cause summary to avoid flooding error-level logs.
		logger.Debug("[cover] ffmpeg stderr for %s:\n%s", audioPath, stderr.String())
		return nil, "", fmt.Errorf("ffmpeg extract cover: %w: %s", err, summarizeFFmpegError(stderr.String()))
	}

	data := stdout.Bytes()
	if len(data) == 0 {
		return nil, "", fmt.Errorf("no cover art found")
	}

	contentType := detectImageType(data)
	return data, contentType, nil
}

// maxFFmpegSummaryRunes bounds the root-cause line kept in the error returned
// to callers, so an error-level log stays short even with a pathological
// stderr. The full stderr is available at debug level (see ExtractFromFile).
const maxFFmpegSummaryRunes = 300

// summarizeFFmpegError condenses ffmpeg's stderr into a single rune-safe line
// for the caller-facing error: the last non-empty line that is not a generic
// trailer ("Conversion failed!" / "Last message repeated ..."), which
// otherwise masks the real root cause. When every line is such a trailer (or
// blank), the last non-empty line is kept as-is so the reason after its colon
// is not lost. A blank stderr yields a generic message.
func summarizeFFmpegError(stderr string) string {
	cause, lastLine := "", ""
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lastLine = line
		if !isFFmpegBoilerplate(line) {
			cause = line
		}
	}
	if cause == "" {
		cause = lastLine
	}
	if cause == "" {
		return "ffmpeg failed"
	}
	return truncateRunes(cause, maxFFmpegSummaryRunes)
}

// isFFmpegBoilerplate reports whether line is a generic trailing diagnostic
// that carries no useful root cause. Lines like "Error while decoding stream
// #0:0: <reason>" are deliberately NOT boilerplate: the text after the colon
// is usually the real failure reason.
func isFFmpegBoilerplate(line string) bool {
	switch line {
	case "Conversion failed!":
		return true
	}
	return strings.HasPrefix(line, "Last message repeated")
}

// truncateRunes cuts s to at most max runes (never splitting a multi-byte
// UTF-8 sequence) and appends an ellipsis when truncated.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
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
			logger.Error("[cover] thumbnail error %s: %v", thumbPath, err)
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
			logger.Info("[cover] remove partial thumbnail %s: %v", outputPath, rerr)
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
			logger.Error("[cover] remove album cover error: %v", err)
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
	logger.Error("[cover] stat error %s: %v", path, err)
	return true
}
