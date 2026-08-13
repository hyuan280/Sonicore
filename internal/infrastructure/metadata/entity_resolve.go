package metadata

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/pkg/utils"
)

// SourceMusicBrainz is the canonical MusicBrainz metadata source name,
// kept in sync with the shared constant in pkg/utils.
const SourceMusicBrainz = utils.SourceMusicBrainz

// EntityResolver centralizes the cross-source lookup/merge chain for artists
// and albums, shared by the scanner and the metadata handlers. It is the
// single place that knows how external IDs and names map onto the entity
// tables (metadata_source + mbid primary, external_ids aliases, normalized
// name fallback).
type EntityResolver struct {
	artists *repository.ArtistRepo
	albums  *repository.AlbumRepo
}

func NewEntityResolver(db *sql.DB) *EntityResolver {
	return &EntityResolver{
		artists: repository.NewArtistRepo(db),
		albums:  repository.NewAlbumRepo(db),
	}
}

// FindArtist resolves an artist through the lookup chain:
//  1. primary ID (metadata_source + mbid)
//  2. external_ids alias reverse lookup
//  3. normalized name — on a hit the incoming (source, externalID) pair is
//     merged into the existing record as an alias (or primary ID when the
//     source matches and the primary ID is empty)
//
// Returns nil when no record matches. A name that normalizes to empty never
// matches.
func (e *EntityResolver) FindArtist(ctx context.Context, source, externalID, name string) (*domain.Artist, error) {
	source = utils.SourceOrDefault(source)

	if externalID != "" {
		if a, err := e.artists.FindBySourceAndID(ctx, source, externalID); err == nil {
			return a, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if a, err := e.artists.FindByExternalID(ctx, source, externalID); err == nil {
			return a, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	if name == "" {
		return nil, nil
	}
	a, err := e.artists.FindByNameNormalized(ctx, name)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, nil
	}
	if err := mergeArtistID(ctx, e.artists, a, source, externalID); err != nil {
		log.Printf("[metadata] merge artist id error: %v", err)
	}
	return a, nil
}

// FindOrCreateArtist resolves like FindArtist and creates a new record when
// nothing matched. The new record is stored under the given source with the
// external ID as its primary identifier.
func (e *EntityResolver) FindOrCreateArtist(ctx context.Context, source, externalID, name string) (*domain.Artist, error) {
	source = utils.SourceOrDefault(source)
	// Apply the placeholder before lookup so an existing "Unknown Artist"
	// row is found by its normalized name and reused instead of creating one
	// row per call.
	if name == "" {
		name = "Unknown Artist"
	}
	a, err := e.FindArtist(ctx, source, externalID, name)
	if err != nil || a != nil {
		return a, err
	}
	now := time.Now()
	a = &domain.Artist{
		ID:             domain.NewID(),
		Name:           name,
		SortName:       name,
		MBID:           externalID,
		MetadataSource: source,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := e.artists.BatchCreate(ctx, []domain.Artist{*a}); err != nil {
		return nil, err
	}
	return a, nil
}

// FindAlbum resolves an album through the lookup chain:
//  1. primary ID (metadata_source + mbid)
//  2. external_ids alias reverse lookup
//  3. normalized title within the owning artist
//
// Returns nil when no record matches.
func (e *EntityResolver) FindAlbum(ctx context.Context, source, externalID, title, artistID string) (*domain.Album, error) {
	source = utils.SourceOrDefault(source)

	if externalID != "" {
		if a, err := e.albums.FindBySourceAndID(ctx, source, externalID); err == nil {
			return a, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if a, err := e.albums.FindByExternalID(ctx, source, externalID); err == nil {
			return a, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	if title == "" || artistID == "" {
		return nil, nil
	}
	a, err := e.albums.FindByTitleNormalizedAndArtist(ctx, title, artistID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, nil
	}
	if err := mergeAlbumID(ctx, e.albums, a, source, externalID); err != nil {
		log.Printf("[metadata] merge album id error: %v", err)
	}
	return a, nil
}

// FindOrCreateAlbum resolves like FindAlbum and creates a new record when
// nothing matched.
func (e *EntityResolver) FindOrCreateAlbum(ctx context.Context, source, externalID, title, artistID string, year int, genre, country string) (*domain.Album, error) {
	source = utils.SourceOrDefault(source)
	// Apply the placeholder before lookup so existing "Unknown Album" rows
	// are reused instead of creating one per track.
	if title == "" {
		title = "Unknown Album"
	}
	a, err := e.FindAlbum(ctx, source, externalID, title, artistID)
	if err != nil || a != nil {
		return a, err
	}
	now := time.Now()
	a = &domain.Album{
		ID:             domain.NewID(),
		Title:          title,
		ArtistID:       artistID,
		MBID:           externalID,
		MetadataSource: source,
		Year:           year,
		Genre:          genre,
		Country:        country,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := e.albums.BatchCreate(ctx, []domain.Album{*a}); err != nil {
		return nil, err
	}
	return a, nil
}

// mergeArtistID records (source, externalID) on an existing artist: as an
// alias when the source differs, as the primary ID when the source matches
// and the primary ID is still empty. On a failed update the in-memory
// mutation is reverted so the returned entity always reflects persisted
// state.
func mergeArtistID(ctx context.Context, repo *repository.ArtistRepo, a *domain.Artist, source, externalID string) error {
	if externalID == "" {
		return nil
	}
	changed := false
	if a.MetadataSource == source {
		if a.MBID == "" {
			a.MBID = externalID
			changed = true
		}
	} else {
		if a.ExternalIDs == nil {
			a.ExternalIDs = map[string]string{source: externalID}
			changed = true
		} else if _, ok := a.ExternalIDs[source]; !ok {
			a.ExternalIDs[source] = externalID
			changed = true
		}
	}
	if changed {
		if err := repo.Update(ctx, a); err != nil {
			if a.MetadataSource == source {
				a.MBID = ""
			} else if a.ExternalIDs != nil {
				delete(a.ExternalIDs, source)
			}
			return err
		}
	}
	return nil
}

// mergeAlbumID records (source, externalID) on an existing album, same
// semantics as mergeArtistID.
func mergeAlbumID(ctx context.Context, repo *repository.AlbumRepo, a *domain.Album, source, externalID string) error {
	if externalID == "" {
		return nil
	}
	changed := false
	if a.MetadataSource == source {
		if a.MBID == "" {
			a.MBID = externalID
			changed = true
		}
	} else {
		if a.ExternalIDs == nil {
			a.ExternalIDs = map[string]string{source: externalID}
			changed = true
		} else if _, ok := a.ExternalIDs[source]; !ok {
			a.ExternalIDs[source] = externalID
			changed = true
		}
	}
	if changed {
		if err := repo.Update(ctx, a); err != nil {
			if a.MetadataSource == source {
				a.MBID = ""
			} else if a.ExternalIDs != nil {
				delete(a.ExternalIDs, source)
			}
			return err
		}
	}
	return nil
}
