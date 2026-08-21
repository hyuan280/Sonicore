package metadata

import (
	"context"
	"sort"

	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/external/netease"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

// BuildRegistry assembles the standard source chain from its configured
// switches: the user metadata cache first (priority 0, authoritative user
// corrections), MusicBrainz next (priority 10), NetEase as the fallback
// (priority 20) when both the metadata switch and a platform provider are
// available. Callers resolve runtime settings (e.g. server_settings
// overrides) before calling. A nil umRepo omits the user source.
func BuildRegistry(mbCfg MBConfig, neProvider *netease.Provider, neEnabled bool, umRepo *repository.UserMetadataRepo) *Registry {
	sources := []port.MetadataSource{NewMBSource(mbCfg)}
	if neEnabled && neProvider != nil {
		sources = append(sources, NewNeteaseSource(neProvider, true))
	}
	sources = append(sources, NewUserSource(umRepo))
	return NewRegistry(sources...)
}

// Registry coordinates multiple MetadataSource implementations into a single
// identification pipeline. Sources are filtered by Enabled() and tried in
// ascending Priority order. Identify runs a field-completion chain: the
// first source with a confident match fixes the track identity, later
// sources fill only the fields that are still missing (and only when their
// capabilities cover a missing field), stopping as soon as every target
// field is present.
type Registry struct {
	sources []port.MetadataSource
}

// NewRegistry keeps only enabled sources and sorts them by ascending
// priority (stable).
func NewRegistry(sources ...port.MetadataSource) *Registry {
	enabled := make([]port.MetadataSource, 0, len(sources))
	for _, s := range sources {
		if s != nil && s.Enabled() {
			enabled = append(enabled, s)
		}
	}
	sort.SliceStable(enabled, func(i, j int) bool {
		return enabled[i].Priority() < enabled[j].Priority()
	})
	return &Registry{sources: enabled}
}

// Sources returns the enabled sources in priority order. Callers must not
// modify the returned slice.
func (r *Registry) Sources() []port.MetadataSource {
	return r.sources
}

// Identify runs the field-completion chain. The first source returning a
// candidate fixes the identity (Source, ExternalID, Title, Artists, Album —
// later sources never overwrite them); subsequent sources are consulted only
// when the still-missing target fields intersect their capabilities, and a
// candidate that is incompatible with the identity (a different song) is
// skipped entirely. A source whose capabilities include a field that only a
// Lookup provides (e.g. NetEase lyrics) gets one extra Lookup call.
//
// Year and Genre are not completion goals: their absence never triggers the
// next source, but candidates that carry them still fill them.
//
// The result is nil when no source produced a candidate; when every source
// failed with an error (e.g. a total upstream outage) and none produced a
// result, the last source error is returned.
func (r *Registry) Identify(ctx context.Context, q port.MetadataQuery) (*port.MetadataCandidate, error) {
	var merged *port.MetadataCandidate
	// Fields the song already gets from the file itself (embedded/sidecar
	// lyrics, embedded cover) count as present and are not completion goals.
	missing := port.TargetFields() &^ q.FileFields.Targets()
	var lastErr error

	for _, s := range r.sources {
		// After the identity source matched, only consult sources whose
		// capabilities cover a still-missing field.
		if merged != nil && missing&s.Capabilities() == 0 {
			continue
		}
		cand, err := s.Identify(ctx, q)
		if err != nil {
			logger.Error("[metadata] source %s identify error: %v", s.Name(), err)
			lastErr = err
			continue
		}
		if cand == nil {
			continue
		}

		// Reject incompatible candidates before the extra Lookup so an
		// unrelated hit does not waste an upstream request.
		if merged != nil && !compatibleCandidates(merged, cand) {
			continue
		}

		// A source whose capabilities cover a still-missing field that its
		// candidate did not provide on first search (e.g. NetEase lyrics)
		// gets one extra Lookup for the full record. Sources that already
		// return the field avoid the extra request.
		needed := missing & s.Capabilities() &^ port.FieldsPresent(cand)
		if needed != 0 && cand.ExternalID != "" {
			if full, ler := s.Lookup(ctx, cand.ExternalID); ler == nil && full != nil {
				// Merge only the still-missing fields so a full record that
				// disagrees with the identity cannot leak unrelated fields in.
				mergeCandidate(full, cand, needed)
			} else if ler != nil {
				logger.Error("[metadata] source %s lookup error: %v", s.Name(), ler)
			}
		}

		if merged == nil {
			// The identity candidate must respect the file-provided fields
			// too: an embedded cover / sidecar lyrics must not be replaced by
			// a platform candidate's values.
			if q.FileFields&port.FileFieldCover != 0 {
				cand.CoverArtURL = ""
			}
			if q.FileFields&port.FileFieldLyrics != 0 {
				cand.Lyrics = ""
			}
			merged = cand
		} else {
			// Exclude fields the file already provides (embedded cover,
			// sidecar/embedded lyrics) so a platform candidate cannot
			// override them — FileFields only trimmed `missing` above but the
			// final merge must not re-add those fields.
			mergeCandidate(cand, merged, port.TargetFields()&^q.FileFields.Targets())
		}
		missing &= ^(port.FieldsPresent(merged) & port.TargetFields())
		if missing == 0 {
			break
		}
	}
	if merged == nil && lastErr != nil {
		return nil, lastErr
	}
	return merged, nil
}

// mergeCandidate copies fields of src into dst that dst does not already
// carry. When mask is non-zero only the masked fields are considered; Year,
// Genre and AlbumCountry are always merged so candidates that carry them
// still fill them. dst fields always win (first source, or earlier sources
// in the chain).
func mergeCandidate(src, dst *port.MetadataCandidate, mask port.MetadataFields) {
	dp := port.FieldsPresent(dst)
	sp := port.FieldsPresent(src)
	if mask == 0 {
		mask = port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum | port.FieldAlbumExternalID |
			port.FieldYear | port.FieldGenre | port.FieldCoverURL | port.FieldLyrics
	}
	if mask&port.FieldTrackID != 0 && dp&port.FieldTrackID == 0 && sp&port.FieldTrackID != 0 &&
		(src.Source == "" || dst.Source == src.Source) {
		dst.ExternalID = src.ExternalID
		// The id lives in src's namespace; carry its source so the final
		// (Source, ExternalID) pair stays consistent for downstream lookups
		// that group by (metadata_source, external_id). The namespace guard
		// above (mirroring the AlbumExternalID branch) refuses to glue a
		// foreign-source id onto an existing destination source.
		if src.Source != "" {
			dst.Source = src.Source
		}
	}
	if mask&port.FieldTitle != 0 && dp&port.FieldTitle == 0 && sp&port.FieldTitle != 0 {
		dst.Title = src.Title
	}
	if mask&port.FieldArtists != 0 && dp&port.FieldArtists == 0 && sp&port.FieldArtists != 0 {
		dst.Artists = src.Artists
	}
	if mask&port.FieldAlbum != 0 && dp&port.FieldAlbum == 0 && sp&port.FieldAlbum != 0 {
		dst.Album = src.Album
	}
	// Album external ID is its own goal, decoupled from the title so a source
	// that set only the album name (e.g. the user cache) still lets a later
	// source fill the platform/MB album id without overwriting the user's
	// title. The id must come from the identity's own source namespace,
	// though: there is no per-field source to record, so adopting an album id
	// from a different source would produce an unresolvable
	// (source, album_external_id) pair downstream.
	if mask&port.FieldAlbumExternalID != 0 && dst.AlbumExternalID == "" && src.AlbumExternalID != "" &&
		(src.Source == "" || dst.Source == src.Source) {
		dst.AlbumExternalID = src.AlbumExternalID
	}
	// Year/Genre are always filled when missing (not completion goals).
	if dp&port.FieldYear == 0 && sp&port.FieldYear != 0 {
		dst.Year = src.Year
	}
	if dp&port.FieldGenre == 0 && sp&port.FieldGenre != 0 {
		dst.Genre = src.Genre
	}
	if dst.AlbumCountry == "" && src.AlbumCountry != "" {
		dst.AlbumCountry = src.AlbumCountry
	}
	if mask&port.FieldCoverURL != 0 && dp&port.FieldCoverURL == 0 && sp&port.FieldCoverURL != 0 {
		dst.CoverArtURL = src.CoverArtURL
	}
	if mask&port.FieldLyrics != 0 && dp&port.FieldLyrics == 0 && sp&port.FieldLyrics != 0 {
		dst.Lyrics = src.Lyrics
	}
}

// compatibleCandidates reports whether the two candidates plausibly refer to
// the same song, so field merging never mixes different tracks.
//
// Rules:
//   - a side that carries no title and no artist info has nothing to
//     contradict, so the other side may fill it (sparse user corrections);
//   - equal normalized titles always match;
//   - containment/token title matches (e.g. "X" inside "X (Live)") require a
//     shared artist, so short titles cannot merge different songs ("Time" vs
//     "One More Time");
//   - any shared artist matches.
func compatibleCandidates(a, b *port.MetadataCandidate) bool {
	if a == nil || b == nil {
		return false
	}
	titleA, titleB := a.Title, b.Title
	na, nb := artistNames(a), artistNames(b)

	// A side with no title and no artist info carries nothing to contradict,
	// so the other side may fill it (sparse user corrections).
	if (titleA == "" && len(na) == 0) || (titleB == "" && len(nb) == 0) {
		return true
	}

	// One side has no title but does carry artists: the other side may fill
	// the title only when the lineups overlap.
	if titleA == "" || titleB == "" {
		return namesOverlap(na, nb)
	}

	// Both sides carry titles.
	if normalizeForMatch(TrimParenSuffix(titleA)) == normalizeForMatch(TrimParenSuffix(titleB)) {
		// Equal titles are not enough when both sides name a different
		// lineup: common song titles ("Time", "Love") exist from many
		// artists, and merging them would graft a different song's
		// album/cover/lyrics/id onto a fixed identity.
		if len(na) > 0 && len(nb) > 0 && !namesOverlap(na, nb) {
			return false
		}
		return true
	}
	// Containment/token title matches need a shared artist so short titles
	// ("Time" vs "One More Time") cannot merge different songs. A pure
	// artist-overlap across two different titles is NOT a match — it would
	// glue a cover/lyrics of a different song onto the identity.
	return titlesMatch(TrimParenSuffix(titleA), TrimParenSuffix(titleB)) && namesOverlap(na, nb)
}

// artistNames returns the normalized artist names of a candidate. A joined
// artist string ("A,B") and a split list ("A", "B") both collapse to the same
// set. A single name is NOT punctuation-split: "AC/DC" stays one unit (as the
// search/score paths already treat it), so "AC" alone cannot produce a false
// overlap with a candidate whose artist is "DC".
func artistNames(c *port.MetadataCandidate) []string {
	var out []string
	for _, ar := range c.Artists {
		if n := normalizeForMatch(TrimParenSuffix(ar.Name)); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func namesOverlap(a, b []string) bool {
	for _, x := range a {
		if x == "" {
			continue
		}
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// SearchCandidates aggregates ranked candidates from every source. A failing
// source is skipped and does not abort the aggregation. When every source
// failed and no candidate was produced, the last source error is returned.
func (r *Registry) SearchCandidates(ctx context.Context, q port.MetadataQuery) ([]port.MetadataCandidate, error) {
	var out []port.MetadataCandidate
	var lastErr error
	for _, s := range r.sources {
		candidates, err := s.SearchCandidates(ctx, q)
		if err != nil {
			logger.Error("[metadata] source %s search error: %v", s.Name(), err)
			lastErr = err
			continue
		}
		out = append(out, candidates...)
	}
	if len(out) == 0 && lastErr != nil {
		return out, lastErr
	}
	return out, nil
}

// Lookup resolves an external ID through the source that owns it (matched by
// source name). Unknown sources yield a nil candidate.
func (r *Registry) Lookup(ctx context.Context, source, externalID string) (*port.MetadataCandidate, error) {
	for _, s := range r.sources {
		if s.Name() == source {
			return s.Lookup(ctx, externalID)
		}
	}
	return nil, nil
}
