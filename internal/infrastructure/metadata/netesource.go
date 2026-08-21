package metadata

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/external/netease"
	"github.com/sonicore/server/internal/infrastructure/logger"
)

// neteaseProvider is the slice of the platform provider the source needs;
// satisfied by *netease.Provider and mockable in tests.
type neteaseProvider interface {
	SearchTracks(ctx context.Context, query string, page, limit int) ([]port.PlatformTrack, int, error)
	EnrichTracks(ctx context.Context, tracks []port.PlatformTrack) ([]port.PlatformTrack, error)
	GetTrack(ctx context.Context, trackID string) (*port.TrackDetail, error)
}

// neteaseSource adapts the NetEase platform provider to the
// port.MetadataSource interface. It backs the registry's fallback chain:
// MusicBrainz is preferred, NetEase covers tracks the former cannot match
// (notably Chinese releases). Identification runs over SearchTracks results
// scored locally; Lookup fetches the full record including lyrics.
type neteaseSource struct {
	name     string
	enabled  bool
	priority int
	provider neteaseProvider
}

// NewNeteaseSource builds a NetEase metadata source around the platform
// provider. When enabled is false the source reports disabled and is skipped
// by the registry.
func NewNeteaseSource(provider *netease.Provider, enabled bool) *neteaseSource {
	return &neteaseSource{
		name:     neteaseSourceName,
		enabled:  enabled,
		priority: neteaseSourcePriority,
		provider: provider,
	}
}

const (
	neteaseSourceName     = "netease"
	neteaseSourcePriority = 20
	// identifyThreshold is the minimum local confidence for Identify to
	// report a match. Scoring gives 0.5 for an exact title and 0.3 for
	// containment, so a bare containment match alone does not pass.
	identifyThreshold = 0.5
)

func (s *neteaseSource) Name() string  { return s.name }
func (s *neteaseSource) Enabled() bool { return s.enabled }
func (s *neteaseSource) Priority() int { return s.priority }

// Capabilities: NetEase provides the network cover URL and lyrics (the
// latter only after a Lookup), but no genre. Search/detail responses expose
// no reliable year (the API's publish time is never populated), so FieldYear
// is intentionally not declared — a source must not claim a field it can
// never deliver.
func (s *neteaseSource) Capabilities() port.MetadataFields {
	return port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum | port.FieldAlbumExternalID |
		port.FieldCoverURL | port.FieldLyrics
}

// Identify searches NetEase for the best scoring candidate and reports it
// when the local confidence clears the threshold. Search responses lack the
// real cover URL and full artist list, so the hits are enriched with one
// batch song-detail request before scoring. A nil result means NetEase had
// no plausible match; errors signal the API is temporarily unavailable.
func (s *neteaseSource) Identify(ctx context.Context, q port.MetadataQuery) (*port.MetadataCandidate, error) {
	// Without a title the local score can reach at most 0.5 (artist + album)
	// — but only when the artist is present too. An empty-title query whose
	// album and artist are both missing can score at most 0.2, below the 0.5
	// threshold, so short-circuit instead of burning a search + batch detail
	// request on a guaranteed miss.
	if q.Title == "" && (q.Album == "" || q.Artist == "") {
		return nil, nil
	}
	query := searchQuery(q)
	if query == "" {
		return nil, nil
	}
	tracks, _, err := s.provider.SearchTracks(ctx, query, 1, 20)
	if err != nil {
		return nil, err
	}
	tracks, err = s.provider.EnrichTracks(ctx, tracks)
	if err != nil {
		return nil, err
	}
	best := bestScored(q, tracks)
	if best == nil {
		logger.Debug("[netease] no match found for %q", query)
		return nil, nil
	}
	logger.Debug("[netease] track matched: %q (id=%s score=%.2f)", best.Title, best.ExternalID, best.Score)
	return best, nil
}

// SearchCandidates maps all search hits to candidates ranked by local
// confidence, best first. Hits are enriched (real cover, full artists)
// through one batch detail request.
func (s *neteaseSource) SearchCandidates(ctx context.Context, q port.MetadataQuery) ([]port.MetadataCandidate, error) {
	query := searchQuery(q)
	if query == "" {
		return nil, nil
	}
	tracks, _, err := s.provider.SearchTracks(ctx, query, 1, 20)
	if err != nil {
		return nil, err
	}
	tracks, err = s.provider.EnrichTracks(ctx, tracks)
	if err != nil {
		return nil, err
	}
	out := make([]port.MetadataCandidate, 0, len(tracks))
	for _, t := range tracks {
		c := neteaseTrackCandidate(t, scoreNeteaseTrack(q, t))
		if c.ExternalID == "" {
			continue
		}
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out, nil
}

// Lookup fetches the full record for a NetEase track id, including lyrics.
// The provider also parses a translated lyric (LyricsTrans) that has no
// carrier in MetadataCandidate, so only the original lyric is surfaced. An
// unresolvable id yields (nil, nil); other failures are real errors.
func (s *neteaseSource) Lookup(ctx context.Context, externalID string) (*port.MetadataCandidate, error) {
	detail, err := s.provider.GetTrack(ctx, externalID)
	if err != nil {
		if errors.Is(err, netease.ErrTrackNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if detail == nil || detail.TrackID == "" {
		return nil, nil
	}
	c := &port.MetadataCandidate{
		Source:          s.name,
		ExternalID:      detail.TrackID,
		Title:           TrimParenSuffix(detail.Title),
		Album:           detail.Album,
		AlbumExternalID: detail.AlbumID,
		CoverArtURL:     detail.CoverURL,
		Lyrics:          detail.Lyrics,
		Score:           1.0,
	}
	if len(detail.Artists) > 0 {
		c.Artists = make([]port.ArtistInfo, 0, len(detail.Artists))
		for _, a := range detail.Artists {
			c.Artists = append(c.Artists, port.ArtistInfo{Name: a.Name, ExternalID: a.ExternalID})
		}
	}
	return c, nil
}

// searchQuery composes the platform search string from the query fields.
// Appending the artist to the title markedly improves NetEase precision for
// common titles. With an empty title the album still joins the search string
// so an "artist + album" query can clear the 0.5 identify threshold.
func searchQuery(q port.MetadataQuery) string {
	q.Title = strings.TrimSpace(q.Title)
	q.Artist = strings.TrimSpace(q.Artist)
	q.Album = strings.TrimSpace(q.Album)
	switch {
	case q.Title == "":
		return strings.TrimSpace(q.Artist + " " + q.Album)
	case q.Artist == "":
		return q.Title
	default:
		return q.Title + " " + q.Artist
	}
}

// scoreNeteaseTrack assigns a [0,1] confidence to a search hit, mirroring
// the MusicBrainz source scoring: 0.5 for an exact title (0.3 for
// containment), up to 0.3 for credited artists, 0.2 for the album.
func scoreNeteaseTrack(q port.MetadataQuery, t port.PlatformTrack) float64 {
	var score float64

	// Normalize the query side symmetrically with the candidate side: a
	// paren-suffixed query title ("晴天 (Live)") must match the candidate's
	// trimmed title exactly instead of falling back to containment scoring.
	qTitle := TrimParenSuffix(q.Title)

	if qTitle != "" {
		if normalizeForMatch(TrimParenSuffix(t.Title)) == normalizeForMatch(qTitle) {
			score += 0.5
		} else if titlesMatch(qTitle, TrimParenSuffix(t.Title)) {
			score += 0.3
		}
	}

	// Artist scoring first tries the whole query artist string as one name:
	// the API returns complete artist names, so a separator-bearing single
	// name like "AC/DC" matches as a unit. The comparison is done on the raw
	// string (lowercase + paren strip only, NO separator folding): folding
	// would collapse a multi-artist tag like "Pink, Floyd" into "pink floyd"
	// and falsely match a single artist named "Pink Floyd". Only when the
	// whole name fails do we fall back to per-separator partial matching.
	whole := strings.ToLower(strings.TrimSpace(TrimParenSuffix(q.Artist)))
	wholeHit := false
	if whole != "" {
		for _, ra := range t.Artists {
			if strings.ToLower(strings.TrimSpace(TrimParenSuffix(ra.Name))) == whole {
				wholeHit = true
				break
			}
		}
	}
	if wholeHit {
		score += 0.3
	} else {
		// Strip the paren suffix before splitting, symmetric with the whole
		// compare above, so an artist like "林俊杰 (Live)" in a multi-artist
		// query matches "林俊杰" instead of failing on the raw suffix.
		queryArtists := splitRawArtists(TrimParenSuffix(q.Artist))
		if len(queryArtists) > 0 && len(t.Artists) > 0 {
			hits := 0
			for _, qa := range queryArtists {
				for _, ra := range t.Artists {
					// ra.Name is a complete artist name from the API's artist
					// array (t.Artist is only a comma-joined derivative); no
					// re-split, so names like "AC/DC" match as a unit.
					if normalizeForMatch(TrimParenSuffix(ra.Name)) == normalizeForMatch(qa) {
						hits++
						break
					}
				}
			}
			score += 0.3 * float64(hits) / float64(len(queryArtists))
		}
	}

	if q.Album != "" && t.Album != "" && titlesMatch(q.Album, t.Album) {
		score += 0.2
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

// bestScored returns the highest-scoring candidate when it clears the
// identify threshold, nil otherwise.
func bestScored(q port.MetadataQuery, tracks []port.PlatformTrack) *port.MetadataCandidate {
	var best *port.MetadataCandidate
	for _, t := range tracks {
		c := neteaseTrackCandidate(t, scoreNeteaseTrack(q, t))
		if c.ExternalID == "" {
			continue
		}
		if best == nil || c.Score > best.Score {
			best = c
		}
	}
	if best != nil && best.Score >= identifyThreshold {
		return best
	}
	return nil
}

func neteaseTrackCandidate(t port.PlatformTrack, score float64) *port.MetadataCandidate {
	c := &port.MetadataCandidate{
		Source:          neteaseSourceName,
		ExternalID:      t.TrackID,
		Title:           TrimParenSuffix(t.Title),
		Album:           t.Album,
		AlbumExternalID: t.AlbumID,
		CoverArtURL:     t.CoverURL,
		Score:           score,
	}
	if len(t.Artists) > 0 {
		c.Artists = make([]port.ArtistInfo, 0, len(t.Artists))
		for _, a := range t.Artists {
			c.Artists = append(c.Artists, port.ArtistInfo{Name: a.Name, ExternalID: a.ExternalID})
		}
	}
	return c
}
