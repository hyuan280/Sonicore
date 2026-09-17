package metadata

import (
	"context"
	"sync"

	"github.com/longbridgeapp/opencc"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/pkg/utils"
)

var (
	openccMu  sync.Mutex
	openccT2S *opencc.OpenCC
)

// openccConverter lazily builds the traditional→simplified converter shared by
// artist name matching. The build is expensive so it is done once and cached;
// a nil result (build failure) is retried on the next call and logged, so a
// transient startup failure does not permanently degrade name matching.
func openccConverter() *opencc.OpenCC {
	if openccT2S != nil {
		return openccT2S
	}
	openccMu.Lock()
	defer openccMu.Unlock()
	if openccT2S != nil {
		return openccT2S
	}
	c, err := opencc.New("t2s")
	if err != nil {
		logger.Error("[artist-image] opencc t2s init failed: %v", err)
		return nil
	}
	openccT2S = c
	return c
}

// maxDisambiguationTitles bounds how many library titles are identified
// through the source when disambiguating same-name artists.
const maxDisambiguationTitles = 3

// maxDisambiguationCandidates bounds how many candidates per title are
// inspected for the artist credit.
const maxDisambiguationCandidates = 3

// normalizeArtistMatchName canonicalizes an artist name for matching: NFKC
// (full-width/half-width), lowercasing, punctuation/symbol stripping, and
// traditional→simplified unification so "周杰倫" matches "周杰伦".
func normalizeArtistMatchName(s string) string {
	s = utils.NormalizeName(s)
	if c := openccConverter(); c != nil {
		if out, err := c.Convert(s); err == nil {
			s = out
		}
	}
	return s
}

// CanonicalMatchArtistName reports whether two artist names refer to the same
// artist under the canonical matching used for name-based avatar resolution.
func CanonicalMatchArtistName(a, b string) bool {
	return normalizeArtistMatchName(a) == normalizeArtistMatchName(b)
}

// artistIDForSource returns the artist's external ID in the given source
// namespace (the primary id when metadata_source matches, otherwise the alias
// in external_ids).
func artistIDForSource(artist *domain.Artist, source string) string {
	if artist == nil || source == "" {
		return ""
	}
	if artist.MetadataSource == source {
		return artist.ExternalID
	}
	return artist.ExternalIDs[source]
}

// ArtistTrackRef identifies one of an artist's library tracks by its stable
// ID (plus title for identification queries and logging).
type ArtistTrackRef struct {
	ID    string
	Title string
}

// ArtistImageResult is the outcome of artist avatar resolution, carrying the
// cover URL plus the external IDs discovered along the way so the caller can
// backfill them and make future resolutions deterministic.
type ArtistImageResult struct {
	CoverURL         string
	Source           string // metadata source that produced the result
	ArtistExternalID string // discovered artist external ID in Source
	TrackID          string // library track ID used for identification
	TrackTitle       string // library title used for track identification
	TrackExternalID  string // discovered track external ID in Source
}

// ResolveArtistImage resolves an artist's avatar, trying the artist's own
// source first and then the remaining image-capable sources in priority order.
// For each source, the artist's known ID in that source is used when present
// (deterministic); otherwise a name search runs. Only when a name search
// discovers the artist AND the source yields a cover is the discovered source
// ID recorded onto the artist's multi-source fields (mutating artist); a
// source that identifies the artist but has no cover is skipped without
// recording its ID.
func ResolveArtistImage(ctx context.Context, sources []port.MetadataSource, artist *domain.Artist, tracks []ArtistTrackRef) ArtistImageResult {
	var res ArtistImageResult
	if artist == nil || len(sources) == 0 {
		return res
	}

	for _, s := range orderSources(sources, artist.MetadataSource) {
		if s.Capabilities()&port.FieldArtistImage == 0 {
			continue
		}
		id := artistIDForSource(artist, s.Name())
		if id != "" {
			// Deterministic: the artist is already keyed under this source, so
			// no new source ID needs recording.
			if d, err := s.LookupArtist(ctx, id); err == nil && d != nil && d.CoverURL != "" {
				logger.Debug("[artist-image] source=%s artist=%q id=%s resolved cover", s.Name(), artist.Name, id)
				res.CoverURL = d.CoverURL
				res.Source = s.Name()
				return res
			}
			logger.Debug("[artist-image] source=%s artist=%q id=%s no cover", s.Name(), artist.Name, id)
			continue
		}
		r := nameSearchArtistImage(ctx, s, artist, tracks)
		if r.CoverURL == "" {
			// This source identified the artist but has no cover to use; its
			// source ID is intentionally not recorded — only a source that
			// actually yields an image is persisted. Keep trying the remaining
			// sources instead of stopping the whole search.
			continue
		}
		if r.ArtistExternalID != "" {
			recordArtistSourceID(artist, s.Name(), r.ArtistExternalID)
			logger.Debug("[artist-image] source=%s artist=%q recorded id=%s into multi-source", s.Name(), artist.Name, r.ArtistExternalID)
		}
		return r
	}

	logger.Debug("[artist-image] artist=%q source=%s no avatar resolved", artist.Name, artist.MetadataSource)
	return res
}

// orderSources returns the image-capable sources ordered so the artist's own
// source is tried first, followed by the rest in their priority order.
func orderSources(sources []port.MetadataSource, own string) []port.MetadataSource {
	if own == "" {
		return sources
	}
	out := make([]port.MetadataSource, 0, len(sources))
	for _, s := range sources {
		if s.Name() == own {
			out = append(out, s)
		}
	}
	for _, s := range sources {
		if s.Name() != own {
			out = append(out, s)
		}
	}
	return out
}

// recordArtistSourceID records a discovered external ID onto an artist: as an
// alias under the source when the primary source differs, or as the primary ID
// when the source matches and the primary is still empty. It mutates artist and
// is a no-op when the ID is already recorded.
func recordArtistSourceID(artist *domain.Artist, source, externalID string) {
	if source == "" || externalID == "" {
		return
	}
	if artist.MetadataSource == source {
		if artist.ExternalID == "" {
			artist.ExternalID = externalID
		}
		return
	}
	if artist.ExternalIDs == nil {
		artist.ExternalIDs = map[string]string{}
	}
	if artist.ExternalIDs[source] == "" {
		artist.ExternalIDs[source] = externalID
	}
}

// nameSearchArtistImage resolves the avatar via name search on a single source.
func nameSearchArtistImage(ctx context.Context, s port.MetadataSource, artist *domain.Artist, tracks []ArtistTrackRef) ArtistImageResult {
	var res ArtistImageResult
	res.Source = s.Name()

	hits, err := s.SearchArtists(ctx, artist.Name)
	if err != nil {
		logger.Debug("[artist-image] source=%s artist=%q name-search error: %v", s.Name(), artist.Name, err)
		return res
	}
	var exact []port.ArtistSearchResult
	for _, h := range hits {
		if CanonicalMatchArtistName(artist.Name, h.Name) {
			exact = append(exact, h)
		}
	}
	logger.Debug("[artist-image] source=%s artist=%q name-search: %d hits, %d exact", s.Name(), artist.Name, len(hits), len(exact))

	switch {
	case len(exact) == 1:
		res.CoverURL = artistCoverFrom(ctx, s, exact[0])
		res.ArtistExternalID = exact[0].ExternalID
		return res
	case len(exact) > 1:
		// Disambiguate: identify one of the artist's library tracks through
		// the source and match the credited artist back to a candidate.
		artistID, libTrack, neTrackID := identifyArtistByTrack(ctx, s, artist.Name, tracks)
		if artistID != "" {
			logger.Debug("[artist-image] source=%s artist=%q disambiguated: artist-id=%s via track %q", s.Name(), artist.Name, artistID, libTrack.Title)
			res.ArtistExternalID = artistID
			res.TrackID = libTrack.ID
			res.TrackTitle = libTrack.Title
			res.TrackExternalID = neTrackID
			for _, h := range exact {
				if h.ExternalID == artistID {
					res.CoverURL = artistCoverFrom(ctx, s, h)
					return res
				}
			}
			if d, err := s.LookupArtist(ctx, artistID); err == nil && d != nil {
				res.CoverURL = d.CoverURL
			}
			return res
		}
		logger.Debug("[artist-image] source=%s artist=%q disambiguation failed", s.Name(), artist.Name)
	}
	return res
}

// identifyArtistByTrack identifies one of the artist's library tracks through
// the source and returns the credited artist's external ID matching the artist
// name, the library track that was used, and the identified platform track ID.
func identifyArtistByTrack(ctx context.Context, s port.MetadataSource, artistName string, tracks []ArtistTrackRef) (artistID string, libTrack ArtistTrackRef, neTrackID string) {
	for i := range tracks {
		if tracks[i].Title == "" || i >= maxDisambiguationTitles {
			continue
		}
		cands, err := s.SearchCandidates(ctx, port.MetadataQuery{Title: tracks[i].Title})
		if err != nil {
			logger.Debug("[artist-image] source=%s identify title %q error: %v", s.Name(), tracks[i].Title, err)
			continue
		}
		for j, c := range cands {
			if j >= maxDisambiguationCandidates {
				break
			}
			for _, a := range c.Artists {
				if a.ExternalID != "" && CanonicalMatchArtistName(a.Name, artistName) {
					return a.ExternalID, tracks[i], c.ExternalID
				}
			}
		}
	}
	return "", ArtistTrackRef{}, ""
}

// artistCoverFrom returns the hit's cover URL, falling back to a full artist
// lookup when the search hit carried none.
func artistCoverFrom(ctx context.Context, s port.MetadataSource, h port.ArtistSearchResult) string {
	if h.CoverURL != "" {
		return h.CoverURL
	}
	if h.ExternalID == "" {
		return ""
	}
	if d, err := s.LookupArtist(ctx, h.ExternalID); err == nil && d != nil {
		return d.CoverURL
	}
	return ""
}
