package port

import "context"

// MetadataQuery is a text-based identification query derived from audio file
// tags or user input. Any field may be empty; sources decide how much of it
// is needed to produce candidates.
type MetadataQuery struct {
	Title  string
	Artist string
	Album  string
}

// ArtistInfo describes one credited artist within a metadata candidate.
type ArtistInfo struct {
	Name       string
	ExternalID string // MusicBrainz MBID, NetEase artist id, etc.
	Country    string // may be empty for sources that do not expose it
}

// MetadataCandidate is a single identification hit from a metadata source.
type MetadataCandidate struct {
	Source          string // matches MetadataSource.Name(), e.g. "musicbrainz"
	ExternalID      string // track-level external identifier (MBID, platform id)
	Title           string
	Artists         []ArtistInfo
	Album           string
	AlbumExternalID string
	Year            int
	Genre           string
	CoverArtURL     string
	Lyrics          string
	Score           float64 // local confidence in [0,1], higher is better
}

// MetadataSource is a pluggable source of track metadata identification
// (MusicBrainz, NetEase Cloud Music, QQ Music, ...).
//
// Implementations must be safe for concurrent use. Priority is ascending:
// lower numbers are tried first by the registry. Enabled reports whether the
// source should be consulted at all (config-dependent).
type MetadataSource interface {
	Name() string
	Enabled() bool
	Priority() int

	// SearchCandidates returns ranked candidates for a query, best first.
	// A nil or empty result means the source found no plausible match.
	// Errors signal the source is temporarily unavailable and should be
	// treated as a skip rather than a failure.
	SearchCandidates(ctx context.Context, q MetadataQuery) ([]MetadataCandidate, error)

	// Identify returns the source's single best match for the query, or nil
	// if none is confident enough. Errors behave like SearchCandidates.
	Identify(ctx context.Context, q MetadataQuery) (*MetadataCandidate, error)

	// Lookup fetches the full record for an external ID previously returned
	// by SearchCandidates or Identify (e.g. an MBID). An unresolvable ID
	// yields (nil, nil) — indistinguishable from a source that has never
	// seen the ID; an error signals the source is temporarily unavailable.
	Lookup(ctx context.Context, externalID string) (*MetadataCandidate, error)
}
