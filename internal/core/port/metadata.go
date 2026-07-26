package port

import (
	"context"

	"github.com/sonicore/server/internal/core/domain"
)

type FileInfo struct {
	Path      string
	Size      int64
	Format    string
	Hash      string
	ModTime   int64
}

type MetadataResult struct {
	Title       string
	Artist      string
	ArtistID    string
	Album       string
	AlbumArtist string
	TrackNumber int
	DiscNumber  int
	Year        int
	Genre       string
	Duration    float64
	BitRate     int
	SampleRate  int
	Channels    int
	Comment     string
	Composer    string
	HasCoverArt bool
	Lyrics      string
	MBID        string
	AcoustID    string
}

type MetadataProvider interface {
	Name() string
	Priority() int
	Identify(ctx context.Context, file *FileInfo) (*MetadataResult, error)
	EnrichTrack(ctx context.Context, track *domain.Track) error
}
