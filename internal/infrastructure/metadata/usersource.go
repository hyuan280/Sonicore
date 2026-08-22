package metadata

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/pkg/utils"
)

// userSource is the user-saved metadata cache as a metadata source. It runs
// first (priority 0) because user-entered corrections are authoritative over
// every network source. The cache records the source it is based on (e.g. a
// track that already had a MusicBrainz id keeps source=musicbrainz + that
// external id) and the source re-provides those.
//
// Lookup is keyed by (user, file hash), so Identify needs the locator fields
// of MetadataQuery; there is no text search or external-id lookup.
type userSource struct {
	name     string
	enabled  bool
	priority int
	repo     *repository.UserMetadataRepo
}

// NewUserSource builds the user metadata source. A nil repo disables it.
func NewUserSource(repo *repository.UserMetadataRepo) *userSource {
	return &userSource{
		name:     "user",
		enabled:  repo != nil,
		priority: 0,
		repo:     repo,
	}
}

func (s *userSource) Name() string  { return s.name }
func (s *userSource) Label() string { return "User" }
func (s *userSource) Enabled() bool { return s.enabled }
func (s *userSource) Priority() int { return s.priority }

// Capabilities: user cache carries identity and text fields, never cover URL
// or lyrics (those are completed by platform sources).
func (s *userSource) Capabilities() port.MetadataFields {
	return port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum |
		port.FieldYear | port.FieldGenre
}

// Identify resolves the saved cache by (user, file hash). A miss yields nil.
func (s *userSource) Identify(ctx context.Context, q port.MetadataQuery) (*port.MetadataCandidate, error) {
	if s.repo == nil || q.FileHash == "" || q.UserID == "" {
		return nil, nil
	}
	um, err := s.repo.FindByUserAndHash(ctx, q.UserID, q.FileHash)
	if err != nil {
		// Only a missing cache row is a clean miss; a real DB failure must
		// surface so the registry can log it and fall through instead of
		// silently dropping the user's authoritative correction.
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// A cache row with no usable fields must not fix identity (it would
	// block the network sources from filling anything); treat it as a miss.
	if um.ExternalID == "" && um.Title == "" && um.Artist == "" &&
		um.Album == "" && um.Year == 0 && um.Genre == "" {
		return nil, nil
	}
	src := um.MetadataSource
	if src == "" {
		src = utils.DefaultSource
	}
	c := &port.MetadataCandidate{
		Source:     src,
		ExternalID: um.ExternalID,
		Title:      um.Title,
		Album:      um.Album,
		Year:       um.Year,
		Genre:      um.Genre,
		Score:      1.0,
	}
	if um.Artist != "" {
		// The cache stores a comma-joined artist list ("A,B") built from
		// individual names by the Save handler, so only a comma split is
		// needed — a multi-separator split would wrongly break names that
		// themselves contain "/", "、" or "&" (e.g. "AC/DC").
		for _, name := range strings.Split(um.Artist, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				c.Artists = append(c.Artists, port.ArtistInfo{Name: name})
			}
		}
	}
	return c, nil
}

// SearchCandidates and Lookup have no meaning for a cache keyed by
// (user, file hash).
func (s *userSource) SearchCandidates(ctx context.Context, q port.MetadataQuery) ([]port.MetadataCandidate, error) {
	return nil, nil
}

func (s *userSource) Lookup(ctx context.Context, externalID string) (*port.MetadataCandidate, error) {
	return nil, nil
}

func (s *userSource) SearchArtists(ctx context.Context, query string) ([]port.ArtistSearchResult, error) {
	return nil, nil
}

func (s *userSource) SearchReleases(ctx context.Context, query string) ([]port.ReleaseSearchResult, error) {
	return nil, nil
}

func (s *userSource) LookupAlbum(ctx context.Context, externalID string) (*port.AlbumDetail, error) {
	return nil, nil
}

func (s *userSource) LookupArtist(ctx context.Context, externalID string) (*port.ArtistLookupDetail, error) {
	return nil, nil
}
