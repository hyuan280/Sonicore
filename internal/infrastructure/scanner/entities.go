package scanner

import (
	"context"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

func (e *Engine) findOrCreateArtist(ctx context.Context, name string, enrichment *metadata.EnrichmentResult) (*domain.Artist, error) {
	return findOrCreateArtist(ctx, e.entities, e.artistRepo, name, enrichment)
}

// findOrCreateArtist resolves an artist through the shared cross-source
// lookup chain (primary ID → alias → normalized name) and backfills
// enrichment fields (external id, country) on an existing record. Artists are
// global across libraries (the resolver and the artists table have no library
// dimension), matching KNOWN_ISSUES #5 where cross-library merging is not
// implemented.
func findOrCreateArtist(ctx context.Context, er *metadata.EntityResolver, artistRepo *repository.ArtistRepo, name string, enrichment *metadata.EnrichmentResult) (*domain.Artist, error) {
	source := ""
	externalID := ""
	if enrichment != nil {
		source = enrichment.Source
		externalID = enrichment.ArtistExternalID
	}

	artist, err := er.FindOrCreateArtist(ctx, source, externalID, name, artistCountry(enrichment))
	if err != nil {
		return nil, err
	}

	if enrichment != nil {
		// Snapshot the pre-backfill values so a failed write rolls the
		// in-memory artist back to the persisted state (matching the album
		// backfill and the mergeArtistID convention).
		oldName, oldSortName, oldCountry := artist.Name, artist.SortName, artist.Country
		updated := false
		if (artist.Name == "" || artist.Name == "Unknown Artist") && enrichment.Artist != "" {
			artist.Name = enrichment.Artist
			artist.SortName = enrichment.Artist
			updated = true
		}
		if artist.Country == "" && enrichment.ArtistCountry != "" {
			artist.Country = enrichment.ArtistCountry
			updated = true
		}
		if updated {
			// Same best-effort semantics as the album backfill: a transient
			// write failure must not drop the track creation upstream (a
			// failed main performer would leave primaryPerformerID empty),
			// so log, roll back and return the artist.
			if err := artistRepo.Update(ctx, artist); err != nil {
				artist.Name, artist.SortName, artist.Country = oldName, oldSortName, oldCountry
				logger.Error("[scan] artist backfill update error: %v", err)
			}
		}
	}
	return artist, nil
}

func (e *Engine) findOrCreateAlbum(ctx context.Context, title, artistID string, year int, genre string, enrichment *metadata.EnrichmentResult) (*domain.Album, error) {
	return findOrCreateAlbum(ctx, e.entities, e.albumRepo, title, artistID, year, genre, enrichment)
}

// findOrCreateAlbum resolves an album through the shared cross-source lookup
// chain (primary ID → alias → normalized title within artist) and backfills
// enrichment fields (external id, year, genre, country). Albums are global
// across libraries, like artists.

func findOrCreateAlbum(ctx context.Context, er *metadata.EntityResolver, albumRepo *repository.AlbumRepo, title, artistID string, year int, genre string, enrichment *metadata.EnrichmentResult) (*domain.Album, error) {
	source := ""
	externalID := ""
	if enrichment != nil {
		source = enrichment.Source
		externalID = enrichment.AlbumExternalID
	}

	// Pre-fill year/genre from the enrichment so a brand-new album is created
	// with its final values (avoiding an INSERT followed by an UPDATE from the
	// backfill below, which then only serves pre-existing records).
	effYear, effGenre := year, genre
	if enrichment != nil {
		if effYear == 0 && enrichment.Year != 0 {
			effYear = enrichment.Year
		}
		if effGenre == "" && enrichment.Genre != "" {
			effGenre = enrichment.Genre
		}
	}

	album, err := er.FindOrCreateAlbum(ctx, source, externalID, title, artistID, effYear, effGenre, albumCountry(enrichment))
	if err != nil {
		return nil, err
	}

	if enrichment != nil {
		// Snapshot the pre-backfill values so a failed write can roll the
		// in-memory object back to match the persisted row (the convention
		// mergeAlbumID follows), instead of handing callers values that were
		// never persisted.
		oldTitle, oldYear, oldGenre, oldCountry := album.Title, album.Year, album.Genre, album.Country
		updated := false
		if album.Title == "Unknown Album" && enrichment.Album != "" {
			album.Title = enrichment.Album
			updated = true
		}
		if album.Year == 0 && enrichment.Year != 0 {
			album.Year = enrichment.Year
			updated = true
		}
		if album.Genre == "" && enrichment.Genre != "" {
			album.Genre = enrichment.Genre
			updated = true
		}
		if album.Country == "" && enrichment.AlbumCountry != "" {
			album.Country = enrichment.AlbumCountry
			updated = true
		}
		if updated {
			// A backfill update is an optional metadata enhancement; a
			// transient write failure must not fail the album (which would
			// drop the whole track creation upstream), so it is logged, the
			// in-memory values are rolled back, and the album is returned.
			if err := albumRepo.Update(ctx, album); err != nil {
				album.Title, album.Year, album.Genre, album.Country = oldTitle, oldYear, oldGenre, oldCountry
				logger.Error("[scan] album backfill update error: %v", err)
			}
		}
	}
	return album, nil
}

func albumCountry(enrichment *metadata.EnrichmentResult) string {
	if enrichment != nil {
		return enrichment.AlbumCountry
	}
	return ""
}

func artistCountry(enrichment *metadata.EnrichmentResult) string {
	if enrichment != nil {
		return enrichment.ArtistCountry
	}
	return ""
}
