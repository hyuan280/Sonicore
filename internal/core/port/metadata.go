package port

import (
	"context"
	"strings"
)

// MetadataQuery is a text-based identification query derived from audio file
// tags or user input. Any field may be empty; sources decide how much of it
// is needed to produce candidates. UserID and FileHash are optional precise
// locators used by the user-metadata source (and ignored by text sources).
type MetadataQuery struct {
	Title    string
	Artist   string
	Album    string
	UserID   string // owner of the saved metadata cache
	FileHash string // file the saved metadata cache applies to

	// TitleFromFilename reports that the title came from the file name (no
	// reliable embedded title tag). It is carried uniformly for every source:
	// sources may relax their matching (e.g. MusicBrainz uses the scored
	// name-derived match instead of an exact tag match) when it is set.
	TitleFromFilename bool

	// FileFields reports which fields the song already gets from the file
	// itself (embedded/sidecar lyrics, embedded cover art). The completion
	// chain treats them as already present, so platform sources are not
	// consulted for them. The dedicated FileFields type (with its own
	// FileField* constants) prevents mixing these up with candidate field
	// presence (FieldsPresent), whose bits carry the opposite meaning for
	// FieldCoverURL.
	FileFields FileFields

	// ExternalID is an exact external identifier from file tags (e.g. MBID).
	// When set together with ExternalSource, the registry's Identify chain
	// tries a Lookup before the text-based search chain.
	ExternalID string
	// ExternalSource is the metadata source namespace for ExternalID.
	ExternalSource string
}

// FileFields is a bitmask of fields a song already gets from its own file.
// It is a distinct type from MetadataFields so a candidate's FieldsPresent()
// result can never be fed in as "file already provides" (or vice versa);
// use the FileField* constants. The bit values coincide with FieldLyrics /
// FieldCoverURL so file-provided fields can be folded into the completion
// goals via Targets().
type FileFields uint32

const (
	FileFieldLyrics FileFields = FileFields(FieldLyrics)
	FileFieldCover  FileFields = FileFields(FieldCoverURL)
)

// Targets folds file-provided fields into the MetadataFields bit space, for
// expressions like `TargetFields() &^ q.FileFields.Targets()`.
func (f FileFields) Targets() MetadataFields { return MetadataFields(f) }

// ArtistInfo describes one credited artist within a metadata candidate.
type ArtistInfo struct {
	Name       string `json:"name"`
	ExternalID string `json:"external_id"` // MusicBrainz MBID, NetEase artist id, etc.
	Country    string `json:"country,omitempty"`
}

// MetadataFields is a bitmask of the metadata fields a source can provide.
// The registry drives its field-completion chain from these capabilities.
type MetadataFields uint32

const (
	FieldTrackID MetadataFields = 1 << iota // track-level external ID
	FieldTitle
	FieldArtists
	FieldAlbum // album title
	FieldAlbumExternalID
	FieldYear
	FieldGenre
	// FieldCoverURL on a candidate means the source provides a network cover
	// URL. It shares its bit value with FileFieldCover (file-provided embedded
	// cover) so the completion goal can exclude file-provided fields, but the
	// two live in distinct types (MetadataFields vs FileFields) and must not
	// be mixed: FieldsPresent(FieldCoverURL) is about a candidate URL, never
	// about what the file already carries.
	FieldCoverURL
	FieldLyrics
	FieldAlbumCountry
)

// TargetFields is the completion goal for the registry chain. Year, Genre and
// AlbumCountry are excluded: their absence never triggers the next source,
// but candidates that carry them still fill them (fields-present check
// below). The album external ID is its own goal so a source that fills only
// the album title (e.g. the user cache) still lets a later source complete
// the platform/MB album id.
func TargetFields() MetadataFields {
	return FieldTrackID | FieldTitle | FieldArtists | FieldAlbum | FieldAlbumExternalID | FieldCoverURL | FieldLyrics
}

// FieldsPresent reports which fields the candidate actually carries values
// for (empty/zero values are treated as absent).
func FieldsPresent(c *MetadataCandidate) MetadataFields {
	var f MetadataFields
	if c == nil {
		return 0
	}
	if c.ExternalID != "" {
		f |= FieldTrackID
	}
	if c.Title != "" {
		f |= FieldTitle
	}
	if len(c.Artists) > 0 {
		for _, a := range c.Artists {
			if strings.TrimSpace(a.Name) != "" {
				f |= FieldArtists
				break
			}
		}
	}
	if c.Album != "" {
		f |= FieldAlbum
	}
	if c.AlbumExternalID != "" {
		f |= FieldAlbumExternalID
	}
	if c.AlbumCountry != "" {
		f |= FieldAlbumCountry
	}
	if c.Year != 0 {
		f |= FieldYear
	}
	if c.Genre != "" {
		f |= FieldGenre
	}
	if c.CoverArtURL != "" {
		f |= FieldCoverURL
	}
	if c.Lyrics != "" {
		f |= FieldLyrics
	}
	return f
}

// MetadataCandidate is a single identification hit from a metadata source.
type MetadataCandidate struct {
	Source          string // matches MetadataSource.Name(), e.g. "musicbrainz"
	ExternalID      string // track-level external identifier (MBID, platform id)
	Title           string
	Artists         []ArtistInfo
	Album           string
	AlbumExternalID string
	AlbumCountry    string
	Year            int
	Genre           string
	CoverArtURL     string
	Lyrics          string
	Score           float64 // local confidence in [0,1], higher is better
}

// ArtistSearchResult is a single artist search hit from a metadata source.
// Source is always set to the producing source's Name().
type ArtistSearchResult struct {
	Name       string `json:"name"`
	ExternalID string `json:"external_id"`
	Country    string `json:"country,omitempty"`
	Type       string `json:"type,omitempty"`
	Source     string `json:"source"`
}

// ReleaseSearchResult is a single release/album search hit from a metadata
// source. Source is always set to the producing source's Name().
type ReleaseSearchResult struct {
	Title      string `json:"title"`
	ExternalID string `json:"external_id"`
	Artist     string `json:"artist,omitempty"`
	Status     string `json:"status,omitempty"`
	Source     string `json:"source"`
}

// ArtistLookupDetail is the result of a platform artist lookup by external ID.
type ArtistLookupDetail struct {
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Country    string `json:"country,omitempty"`
	Type       string `json:"type,omitempty"`
}

// AlbumDetail is the result of a platform album lookup by external ID.
type AlbumDetail struct {
	ExternalID string `json:"external_id"`
	Title      string `json:"title"`
	ArtistName string `json:"artist_name,omitempty"`
	ArtistID   string `json:"artist_id,omitempty"`
	Year       int    `json:"year,omitempty"`
	Genre      string `json:"genre,omitempty"`
	Country    string `json:"country,omitempty"`
}

// MetadataSource is a pluggable source of track metadata identification
// (user metadata cache, MusicBrainz, NetEase Cloud Music, ...).
//
// Implementations must be safe for concurrent use. Priority is ascending:
// lower numbers are tried first by the registry. Enabled reports whether the
// source should be consulted at all (config-dependent).
type MetadataSource interface {
	Name() string
	Label() string
	Enabled() bool
	Priority() int

	// Capabilities declares which fields the source can provide. The
	// registry uses it to decide whether the source should be consulted for
	// the still-missing fields.
	Capabilities() MetadataFields

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

	// SearchArtists searches for artists by name. The source that does not
	// support artist search returns (nil, nil). Errors signal the source is
	// temporarily unavailable.
	SearchArtists(ctx context.Context, query string) ([]ArtistSearchResult, error)

	// SearchReleases searches for releases/albums by name. The source that
	// does not support release search returns (nil, nil). Errors signal the
	// source is temporarily unavailable.
	SearchReleases(ctx context.Context, query string) ([]ReleaseSearchResult, error)

	// LookupAlbum fetches album details (year, genre, country, artist) by
	// the platform's external album ID. A source that does not support album
	// lookup returns (nil, nil). Errors signal the source is temporarily
	// unavailable.
	LookupAlbum(ctx context.Context, externalID string) (*AlbumDetail, error)

	// LookupArtist fetches artist details (name, country, type) by the
	// platform's external artist ID. A source that does not support artist
	// lookup returns (nil, nil). Errors signal the source is temporarily
	// unavailable.
	LookupArtist(ctx context.Context, externalID string) (*ArtistLookupDetail, error)
}
