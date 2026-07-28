package metadata

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/image/draw"
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
		ResizeToThumbnail(data, thumbPath, size)
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
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Printf("[cover] image.Decode error: %v", err)
		return
	}

	bounds := src.Bounds()
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

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	out, err := os.Create(outputPath)
	if err != nil {
		log.Printf("[cover] create thumbnail error: %v", err)
		return
	}
	defer out.Close()
	jpeg.Encode(out, dst, &jpeg.Options{Quality: 85})
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
