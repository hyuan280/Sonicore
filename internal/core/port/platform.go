package port

import "context"

// Chart describes a playable chart/ranking from an external music platform.
type Chart struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	CoverURL     string `json:"cover_url,omitempty"`
	TrackCount   int    `json:"track_count"`
	UpdateFreq   string `json:"update_freq,omitempty"`
}

// PlatformTrack is a lightweight track reference as surfaced by search or
// chart browsing on an external platform. Duration is in seconds.
type PlatformTrack struct {
	Platform  string  `json:"platform"`
	TrackID   string  `json:"track_id"`
	Title     string  `json:"title"`
	Artist    string  `json:"artist"`
	ArtistID  string  `json:"artist_id"`
	Album     string  `json:"album"`
	AlbumID   string  `json:"album_id"`
	Duration  float64 `json:"duration"`
	CoverURL  string  `json:"cover_url,omitempty"`
}

// TrackDetail is the full detail view of a platform track.
// Duration is in seconds.
type TrackDetail struct {
	Platform    string  `json:"platform"`
	TrackID     string  `json:"track_id"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	ArtistID    string  `json:"artist_id"`
	Album       string  `json:"album"`
	AlbumID     string  `json:"album_id"`
	Duration    float64 `json:"duration"`
	CoverURL    string  `json:"cover_url,omitempty"`
	Year        int     `json:"year,omitempty"`
	Genre       string  `json:"genre,omitempty"`
	Lyrics      string  `json:"lyrics,omitempty"`
	LyricsTrans  string  `json:"lyrics_translation,omitempty"`
	PublishTime string  `json:"publish_time,omitempty"`
}

// ArtistDetail is the detail view of a platform artist.
type ArtistDetail struct {
	Platform   string `json:"platform"`
	ArtistID   string `json:"artist_id"`
	Name       string `json:"name"`
	CoverURL   string `json:"cover_url,omitempty"`
	AlbumCount int    `json:"album_count"`
	TrackCount int    `json:"track_count"`
	BriefDesc  string `json:"brief_desc,omitempty"`
}

// PlatformProvider is a pluggable source of external music platform data
// (charts, search, track/artist details).
//
// Pagination conventions for GetChart/SearchTracks/SearchArtists/
// GetArtistTracks: page is 1-based; limit is the per-page size (providers
// clamp it); the second return value is the total number of matching items,
// not the length of the returned slice. Not-found is reported as an error,
// and an empty result is returned as an empty slice with total 0.
type PlatformProvider interface {
	Name() string

	// Label is the human-readable platform name shown in the UI
	// (e.g. "网易云音乐").
	Label() string

	ListCharts(ctx context.Context) ([]Chart, error)
	GetChart(ctx context.Context, chartID string, page, limit int) ([]PlatformTrack, int, error)

	SearchTracks(ctx context.Context, query string, page, limit int) ([]PlatformTrack, int, error)
	SearchArtists(ctx context.Context, query string, page, limit int) ([]ArtistDetail, int, error)

	GetTrack(ctx context.Context, trackID string) (*TrackDetail, error)
	GetArtist(ctx context.Context, artistID string) (*ArtistDetail, error)
	GetArtistTracks(ctx context.Context, artistID string, page, limit int) ([]PlatformTrack, int, error)
}
