package metadata

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type CoverExtractor struct {
	imagesDir string
}

func NewCoverExtractor(imagesDir string) *CoverExtractor {
	return &CoverExtractor{imagesDir: imagesDir}
}

func (ce *CoverExtractor) ExtractFromFile(audioPath string) ([]byte, string, error) {
	cmd := exec.Command("ffmpeg",
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

func (ce *CoverExtractor) Save(libraryID, ownerType, ownerID string, data []byte, ext string) (string, error) {
	dir := filepath.Join(ce.imagesDir, libraryID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	filename := fmt.Sprintf("%s_%s.%s", ownerType, ownerID, ext)
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	thumbnailPath := filepath.Join(dir, fmt.Sprintf("%s_%s_256.jpg", ownerType, ownerID))
	ResizeToThumbnail(data, thumbnailPath, 256)
	if _, err := os.Stat(thumbnailPath); err != nil {
		log.Printf("[cover] thumbnail NOT created at %s", thumbnailPath)
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

func ResizeToThumbnail(data []byte, outputPath string, maxSize int) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Printf("[cover] image.Decode error: %v", err)
		return
	}

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	if w <= maxSize && h <= maxSize {
		return
	}

	scale := float64(maxSize) / float64(w)
	if h > w {
		scale = float64(maxSize) / float64(h)
	}

	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)

	cmd := exec.Command("ffmpeg",
		"-y",
		"-f", "image2pipe",
		"-i", "pipe:0",
		"-vf", fmt.Sprintf("scale=%d:%d", newW, newH),
		"-q:v", "2",
		outputPath,
	)
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[cover] ffmpeg resize error: %v\n%s", err, out)
	}
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
