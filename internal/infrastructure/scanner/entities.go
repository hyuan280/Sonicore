package scanner

import (
	"context"

	"github.com/sonicore/server/internal/core/domain"
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
	source := metadata.SourceMusicBrainz
	externalID := ""
	if enrichment != nil {
		source = enrichment.Source
		externalID = enrichment.ArtistExternalID
	}
	if source == "" {
		source = metadata.SourceMusicBrainz
	}

	artist, err := er.FindOrCreateArtist(ctx, source, externalID, name)
	if err != nil {
		return nil, err
	}

	if enrichment != nil {
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
			if err := artistRepo.Update(ctx, artist); err != nil {
				return nil, err
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
	source := metadata.SourceMusicBrainz
	externalID := ""
	if enrichment != nil {
		source = enrichment.Source
		externalID = enrichment.AlbumExternalID
	}
	if source == "" {
		source = metadata.SourceMusicBrainz
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
			if err := albumRepo.Update(ctx, album); err != nil {
				return nil, err
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
