package metadata

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sonicore/server/internal/core/port"
)

// mbSource adapts the MusicBrainz resolver to the port.MetadataSource
// interface. The recognition chain (search + exact/scored/artist matching)
// lives in Resolver; this adapter exposes it as a pluggable source with
// per-candidate confidence scores.
type mbSource struct {
	name     string
	enabled  bool
	priority int
	resolver *Resolver
	mb       *MBClient
}

// NewMBSource builds a MusicBrainz metadata source. When cfg.Enabled is
// false the source reports disabled and is skipped by the registry.
func NewMBSource(cfg MBConfig) *mbSource {
	return &mbSource{
		name:     "musicbrainz",
		enabled:  cfg.Enabled,
		priority: 10,
		resolver: NewResolver(cfg),
		mb:       NewMBClient(cfg),
	}
}

func (s *mbSource) Name() string  { return s.name }
func (s *mbSource) Enabled() bool { return s.enabled }
func (s *mbSource) Priority() int { return s.priority }

// Capabilities: MusicBrainz has no network cover URL or lyrics. This is a
// deliberate decision, not an omission: candidates never carry a
// coverartarchive URL and FetchCoverArt is not wired into the cover chain.
// A MusicBrainz track without an embedded cover therefore stays coverless in
// MB-only deployments (accepted downgrade; NetEase covers, when enabled, fill
// the gap via the registry's SearchCandidates).
func (s *mbSource) Capabilities() port.MetadataFields {
	return port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum | port.FieldAlbumExternalID |
		port.FieldYear | port.FieldGenre
}

// Identify runs the existing recognition chain and reports the matched
// recording with full confidence (the chain already decided it was the best).
func (s *mbSource) Identify(ctx context.Context, q port.MetadataQuery) (*port.MetadataCandidate, error) {
	result, err := s.resolver.Enrich(ctx, &AudioMeta{
		Title:             q.Title,
		Artist:            q.Artist,
		Album:             q.Album,
		TitleFromFilename: q.TitleFromFilename,
	})
	if err != nil || result == nil {
		return nil, err
	}
	return enrichmentToCandidate(s.name, result), nil
}

// Lookup resolves a recording MBID to a full candidate. An unresolvable ID
// (MusicBrainz 404) yields (nil, nil); other failures are real errors.
func (s *mbSource) Lookup(ctx context.Context, externalID string) (*port.MetadataCandidate, error) {
	result, err := s.resolver.IdentifyTrack(ctx, externalID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil || result == nil {
		return nil, err
	}
	return enrichmentToCandidate(s.name, result), nil
}

// SearchCandidates lists search results scored locally (best first) without
// running the full enrichment chain.
func (s *mbSource) SearchCandidates(ctx context.Context, q port.MetadataQuery) ([]port.MetadataCandidate, error) {
	recordings, err := s.mb.SearchRecordings(ctx, q.Title, splitRawArtists(q.Artist), q.Album)
	if err != nil {
		return nil, err
	}

	out := make([]port.MetadataCandidate, 0, len(recordings))
	for _, rec := range recordings {
		c := recordingToCandidate(s.name, rec, s.scoreRecording(q, &rec))
		if c.ExternalID == "" {
			continue
		}
		out = append(out, c)
	}
	// MB returns results in its own relevance order; re-rank by local
	// confidence so the "best first" contract holds regardless.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out, nil
}

// scoreRecording assigns a [0,1] confidence to a search result based on how
// closely its title/artist/album line up with the query. 0.5 points for an
// exact title (0.3 for a containment match), 0.3 for credited artists, 0.2
// for the album.
func (s *mbSource) scoreRecording(q port.MetadataQuery, rec *MBRecording) float64 {
	var score float64

	if q.Title != "" {
		if normalizeForMatch(TrimParenSuffix(rec.Title)) == normalizeForMatch(q.Title) {
			score += 0.5
		} else if titlesMatch(q.Title, TrimParenSuffix(rec.Title)) {
			score += 0.3
		}
	}

	queryArtists := splitRawArtists(q.Artist)
	if len(queryArtists) > 0 && len(rec.Artists) > 0 {
		hits := 0
		for _, qa := range queryArtists {
			for _, ra := range rec.Artists {
				if normalizeForMatch(TrimParenSuffix(ra.Name)) == normalizeForMatch(qa) {
					hits++
					break
				}
			}
		}
		score += 0.3 * float64(hits) / float64(len(queryArtists))
	}

	if q.Album != "" {
		for _, rel := range rec.Releases {
			// Keep the album bonus in sync with recordingToCandidate, which
			// only surfaces official (or status-less) releases.
			if rel.Status != "" && !strings.EqualFold(rel.Status, "official") {
				continue
			}
			if titlesMatch(q.Album, rel.Title) {
				score += 0.2
				break
			}
		}
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

func enrichmentToCandidate(source string, r *EnrichmentResult) *port.MetadataCandidate {
	if r == nil {
		return nil
	}
	c := &port.MetadataCandidate{
		Source:          source,
		ExternalID:      r.TrackExternalID,
		Title:           r.Title,
		Album:           r.Album,
		AlbumExternalID: r.AlbumExternalID,
		AlbumCountry:    r.AlbumCountry,
		Year:            r.Year,
		Genre:           r.Genre,
		Score:           1.0,
	}
	for _, ar := range r.Artists {
		c.Artists = append(c.Artists, port.ArtistInfo{Name: ar.Name, ExternalID: ar.ExternalID, Country: ar.Country})
	}
	return c
}

// CandidateToEnrichment adapts a registry candidate back into the
// enrichment shape the scanner consumes. The candidate's Source is
// preserved so entity creation/update paths record the right metadata
// source instead of assuming MusicBrainz.
func CandidateToEnrichment(c *port.MetadataCandidate) *EnrichmentResult {
	if c == nil {
		return nil
	}
	r := &EnrichmentResult{
		Source:          c.Source,
		TrackExternalID: c.ExternalID,
		AlbumExternalID: c.AlbumExternalID,
		AlbumCountry:    c.AlbumCountry,
		Title:           c.Title,
		Album:           c.Album,
		Year:            c.Year,
		Genre:           c.Genre,
		Lyrics:          c.Lyrics,
	}
	for _, a := range c.Artists {
		r.Artists = append(r.Artists, ArtistResult{Name: a.Name, ExternalID: a.ExternalID, Country: a.Country})
	}
	if len(r.Artists) > 0 {
		r.Artist = r.Artists[0].Name
		r.ArtistExternalID = r.Artists[0].ExternalID
		r.ArtistCountry = r.Artists[0].Country
	}
	return r
}

func recordingToCandidate(source string, rec MBRecording, score float64) port.MetadataCandidate {
	c := port.MetadataCandidate{
		Source:     source,
		ExternalID: rec.ID,
		Title:      TrimParenSuffix(rec.Title),
		Score:      score,
	}
	for _, ref := range rec.Artists {
		ai := port.ArtistInfo{Name: TrimParenSuffix(ref.Name)}
		if ref.Artist != nil {
			ai.ExternalID = ref.Artist.ID
			ai.Country = ref.Artist.Country
		}
		if ai.Name != "" {
			c.Artists = append(c.Artists, ai)
		}
	}
	for _, rel := range rec.Releases {
		if rel.Status != "" && !strings.EqualFold(rel.Status, "official") {
			continue
		}
		c.Album = TrimParenSuffix(rel.Title)
		c.AlbumExternalID = rel.ID
		if len(rel.Date) >= 4 {
			fmt.Sscanf(rel.Date[:4], "%d", &c.Year)
		}
		break
	}
	return c
}

// splitRawArtists splits a single artist string into raw names on the
// separators used by Resolver (commas, slashes, 、). "&" is left intact so
// band names like "A & B" survive.
func splitRawArtists(s string) []string {
	var parts []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == ',' || r == '、'
	}) {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}
