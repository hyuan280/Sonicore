package port

import (
	"context"

	"github.com/sonicore/server/internal/core/domain"
)

type SourceInfo struct {
	Name      string
	URL       string
	Formats   []FormatInfo
	Thumbnail string
	Title     string
	Duration  float64
}

type FormatInfo struct {
	ID          string
	Extension   string
	BitRate     int
	FileSize    int64
	Description string
}

type DownloadSource interface {
	Name() string
	Match(url string) bool
	Resolve(ctx context.Context, url string) (*SourceInfo, error)
	Fetch(ctx context.Context, job *domain.DownloadJob) error
}
