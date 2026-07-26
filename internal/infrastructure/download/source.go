package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
)

type DirectSource struct {
	client *http.Client
}

func NewDirectSource() *DirectSource {
	return &DirectSource{
		client: &http.Client{
			Timeout: 30 * time.Minute,
		},
	}
}

func (s *DirectSource) Name() string { return "direct" }

func (s *DirectSource) Match(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

func (s *DirectSource) Resolve(ctx context.Context, url string) (*port.SourceInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Sonicore/0.1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HEAD failed: %w", err)
	}
	defer resp.Body.Close()

	info := &port.SourceInfo{
		Name: s.Name(),
		URL:  url,
	}

	ext := detectExt(resp.Header.Get("Content-Type"), url)
	info.Formats = []port.FormatInfo{
		{
			ID:        "original",
			Extension: ext,
			FileSize:  resp.ContentLength,
		},
	}

	return info, nil
}

func (s *DirectSource) Fetch(ctx context.Context, job *domain.DownloadJob) error {
	req, err := http.NewRequestWithContext(ctx, "GET", job.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Sonicore/0.1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	ext := detectExt(resp.Header.Get("Content-Type"), job.URL)
	outPath := job.TargetPath
	if outPath == "" || strings.HasSuffix(outPath, "/") {
		outPath = filepath.Join(outPath, extractFilename(job.URL, ext))
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}

	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	job.TargetPath = outPath
	job.Progress = 100

	_ = written
	return nil
}

func detectExt(contentType, url string) string {
	ext := filepath.Ext(url)
	if ext != "" {
		return strings.TrimPrefix(ext, ".")
	}

	switch {
	case strings.Contains(contentType, "audio/mpeg"):
		return "mp3"
	case strings.Contains(contentType, "audio/flac"):
		return "flac"
	case strings.Contains(contentType, "audio/ogg"):
		return "ogg"
	case strings.Contains(contentType, "audio/mp4"):
		return "m4a"
	case strings.Contains(contentType, "audio/wav"):
		return "wav"
	case strings.Contains(contentType, "audio/x-wav"):
		return "wav"
	default:
		return "bin"
	}
}

func extractFilename(urlStr, ext string) string {
	parts := strings.Split(urlStr, "/")
	name := parts[len(parts)-1]
	if strings.Contains(name, "?") {
		name = name[:strings.Index(name, "?")]
	}
	if !strings.Contains(name, ".") {
		name = name + "." + ext
	}
	return name
}
