package scanner

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/lib/pq"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type Engine struct {
	db             *sql.DB
	trackRepo      *repository.TrackRepo
	albumRepo      *repository.AlbumRepo
	artistRepo     *repository.ArtistRepo
	umRepo         *repository.UserMetadataRepo
	coverExtractor *metadata.CoverExtractor
	resolver       *metadata.Resolver
}

func NewEngine(db *sql.DB, imagesDir string, mbCfg metadata.MBConfig) *Engine {
	var resolver *metadata.Resolver
	if mbCfg.Enabled {
		resolver = metadata.NewResolver(mbCfg)
		log.Printf("[scanner] MusicBrainz resolver enabled: %s (rate: %d/s)", mbCfg.APIURL, mbCfg.RateLimit)
	} else {
		log.Printf("[scanner] MusicBrainz resolver disabled")
	}
	return &Engine{
		db:             db,
		trackRepo:      repository.NewTrackRepo(db),
		albumRepo:      repository.NewAlbumRepo(db),
		artistRepo:     repository.NewArtistRepo(db),
		umRepo:         repository.NewUserMetadataRepo(db),
		coverExtractor: metadata.NewCoverExtractor(imagesDir),
		resolver:       resolver,
	}
}

type ScanOptions struct {
	Mode string
}

type ScanStats struct {
	TotalFiles      int
	Scanned         int
	NewTracks       int
	UpdatedTracks   int
	DeletedTracks   int
	CoversExtracted int
	Errors          []string
}

func (e *Engine) ScanLibrary(ctx context.Context, lib *domain.Library, opts ScanOptions, onProgress func(stats ScanStats)) (*ScanStats, error) {
	stats := &ScanStats{}

	_ = filepath.Walk(lib.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && metadata.IsAudioFile(path) {
			stats.TotalFiles++
		}
		return nil
	})
	onProgress(*stats)

	existingTracks, err := e.trackRepo.FindByLibraryID(ctx, lib.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load existing tracks: %w", err)
	}

	existingByPath := make(map[string]*domain.Track, len(existingTracks))
	for i := range existingTracks {
		existingByPath[existingTracks[i].FilePath] = &existingTracks[i]
	}

	seenPaths := make(map[string]bool)

	err = filepath.Walk(lib.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("walk error %s: %v", path, err))
			return nil
		}
		if info.IsDir() || !metadata.IsAudioFile(path) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		meta, err := metadata.Probe(path)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("probe error %s: %v", path, err))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		fileHash, err := hashFile(path)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("hash error %s: %v", path, err))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		// Apply user-saved metadata overrides (priority 1)
		if um, err := e.umRepo.FindByUserAndHash(ctx, lib.OwnerID, fileHash); err == nil {
			if um.Title != "" {
				meta.Title = um.Title
				meta.TitleFromFilename = false
			}
			if um.Artist != "" {
				meta.Artist = um.Artist
			}
			if um.Album != "" {
				meta.Album = um.Album
			}
			if um.Year != 0 {
				meta.Year = um.Year
			}
			if um.Genre != "" {
				meta.Genre = um.Genre
			}
		}

		seenPaths[path] = true

		var existing *domain.Track
		if e, ok := existingByPath[path]; ok {
			existing = e
		}

		var enrichment *metadata.EnrichmentResult
		if e.resolver != nil && (opts.Mode == "overwrite" || !e.metaComplete(ctx, existing)) {
			if result, err := e.resolver.Enrich(ctx, meta); err != nil {
				log.Printf("[scan] mb enrich error for %s: %v", path, err)
			} else {
				enrichment = result
				if enrichment != nil {
					log.Printf("[scan] mb enriched: artist_mbid=%s album_mbid=%s track_mbid=%s genre=%s year=%d",
						enrichment.ArtistMBID, enrichment.AlbumMBID, enrichment.TrackMBID, enrichment.Genre, enrichment.Year)
				}
			}
		}

		if existing != nil && existing.Hash == fileHash {
			changed := false
			overwrite := opts.Mode == "overwrite"

			if meta.MBID != "" && (existing.MBID == "" || overwrite) {
				existing.MBID = meta.MBID
				changed = true
			}
			if enrichment != nil {
				if meta.TitleFromFilename && enrichment.Title != "" && (existing.Title != enrichment.Title || overwrite) {
					existing.Title = enrichment.Title
					changed = true
				}
				if enrichment.TrackMBID != "" && (existing.MBID == "" || overwrite) {
					existing.MBID = enrichment.TrackMBID
					changed = true
				}
				if enrichment.ArtistMBID != "" && existing.ArtistID != "" {
					if artist, err := e.artistRepo.FindByID(ctx, existing.ArtistID); err == nil {
						updated := false
						if enrichment.ArtistMBID != "" && (artist.MBID == "" || overwrite) {
							artist.MBID = enrichment.ArtistMBID
							updated = true
						}
						if enrichment.Artist != "" && (artist.Name == "" || artist.Name == "Unknown Artist" || overwrite) {
							artist.Name = enrichment.Artist
							artist.SortName = enrichment.Artist
							updated = true
						}
						if enrichment.ArtistCountry != "" && (artist.Country == "" || overwrite) {
							artist.Country = enrichment.ArtistCountry
							updated = true
						}
						if updated {
							changed = true
							e.artistRepo.Update(ctx, artist)
						}
					}
				}
				if enrichment.AlbumMBID != "" && existing.AlbumID != "" {
					if album, err := e.albumRepo.FindByID(ctx, existing.AlbumID); err == nil {
						updated := false
						if enrichment.AlbumMBID != "" && (album.MBID == "" || overwrite) {
							album.MBID = enrichment.AlbumMBID
							updated = true
						}
						if enrichment.Album != "" && (album.Title == "" || album.Title == "Unknown Album" || overwrite) {
							album.Title = enrichment.Album
							updated = true
						}
						if enrichment.Year != 0 && (album.Year == 0 || overwrite) {
							album.Year = enrichment.Year
							updated = true
						}
						if enrichment.Genre != "" && (album.Genre == "" || overwrite) {
							album.Genre = enrichment.Genre
							updated = true
						}
						if enrichment.AlbumCountry != "" && (album.Country == "" || overwrite) {
							album.Country = enrichment.AlbumCountry
							updated = true
						}
						if updated {
							changed = true
							e.albumRepo.Update(ctx, album)
						}
					}
				}
			}
			if meta.HasCoverArt {
				if !thumbnailExists(e.coverExtractor, lib.ID, existing.ID, 64) {
					if data, _, err := e.coverExtractor.ExtractFromFile(path); err == nil {
						thumbDir := filepath.Join(e.coverExtractor.ImagesDir(), lib.ID)
						os.MkdirAll(thumbDir, 0755)
						thumbPath := filepath.Join(thumbDir, fmt.Sprintf("track_%s_64.jpg", existing.ID))
						metadata.ResizeToThumbnail(data, thumbPath, 64)
						if existing.CoverImageID == nil {
							existing.CoverImageID = &existing.ID
							changed = true
						}
						stats.CoversExtracted++
					}
				}
			}
			if changed {
				e.trackRepo.Update(ctx, existing)
			}
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		title := meta.Title
		if meta.TitleFromFilename && enrichment != nil && enrichment.Title != "" {
			title = enrichment.Title
		}

		artistName := meta.Artist
		if meta.Artist == "" && enrichment != nil && enrichment.Artist != "" {
			artistName = enrichment.Artist
		}

		albumName := meta.Album
		if meta.Album == "" && enrichment != nil && enrichment.Album != "" {
			albumName = enrichment.Album
		}

		year := meta.Year
		genre := meta.Genre

		artist, err := e.findOrCreateArtist(ctx, lib.ID, artistName, enrichment)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("artist error: %v", err))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		album, err := e.findOrCreateAlbum(ctx, lib.ID, albumName, artist.ID, year, genre, enrichment)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("album error: %v", err))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		now := time.Now()
		trackID := domain.NewID()
		if existing != nil {
			trackID = existing.ID
		}

		track := &domain.Track{
			ID:          trackID,
			LibraryID:   lib.ID,
			Title:       title,
			AlbumID:     album.ID,
			ArtistID:    artist.ID,
			TrackNumber: meta.TrackNumber,
			DiscNumber:  meta.DiscNumber,
			Duration:    meta.Duration,
			BitRate:     meta.BitRate,
			SampleRate:  meta.SampleRate,
			Channels:    meta.Channels,
			FilePath:    path,
			FileSize:    meta.FileSize,
			FileFormat:  meta.FileFormat,
			Hash:        fileHash,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		if enrichment != nil {
			track.MBID = enrichment.TrackMBID
		}
		if meta.MBID != "" {
			track.MBID = meta.MBID
		}

		if existing != nil {
			track.CreatedAt = existing.CreatedAt
			track.Rating = existing.Rating
			track.PlayCount = existing.PlayCount
			track.LastPlayedAt = existing.LastPlayedAt
			track.CoverImageID = existing.CoverImageID
		}

		if meta.HasCoverArt {
			if !thumbnailExists(e.coverExtractor, lib.ID, trackID, 64) {
				if data, _, err := e.coverExtractor.ExtractFromFile(path); err == nil {
					thumbDir := filepath.Join(e.coverExtractor.ImagesDir(), lib.ID)
					os.MkdirAll(thumbDir, 0755)
					thumbPath := filepath.Join(thumbDir, fmt.Sprintf("track_%s_64.jpg", trackID))
					metadata.ResizeToThumbnail(data, thumbPath, 64)
					track.CoverImageID = &trackID
					stats.CoversExtracted++
					if album != nil && album.CoverImageID == nil {
						e.coverExtractor.Save("album", "album", album.ID, data, "jpg", 256)
						album.CoverImageID = &album.ID
						e.albumRepo.Update(ctx, album)
					}
				}
			}
		}

		if existing != nil {
			if err := e.trackRepo.Update(ctx, track); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("update error %s: %v", path, err))
			} else {
				stats.UpdatedTracks++
			}
		} else {
			if err := e.trackRepo.BatchCreate(ctx, []domain.Track{*track}); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("create error %s: %v", path, err))
			} else {
				stats.NewTracks++
			}
		}

		stats.Scanned++
		onProgress(*stats)
		return nil
	})
	if err != nil {
		return stats, err
	}

	var deletedIDs []string
	for path := range existingByPath {
		if !seenPaths[path] {
			trackID, err := e.trackRepo.DeleteByFilePath(ctx, path, lib.ID)
			if err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("delete error %s: %v", path, err))
			} else if trackID != "" {
				deletedIDs = append(deletedIDs, trackID)
				stats.DeletedTracks++
			}
		}
	}

	if len(deletedIDs) > 0 {
		e.db.ExecContext(ctx, `DELETE FROM favorites WHERE item_type = 'track' AND item_id = ANY($1)`, pq.Array(deletedIDs))
		e.db.ExecContext(ctx, `DELETE FROM play_history WHERE track_id = ANY($1)`, pq.Array(deletedIDs))
		e.db.ExecContext(ctx,
			`UPDATE playlists SET track_ids = (
				SELECT COALESCE(jsonb_agg(elem), '[]'::jsonb)
				FROM jsonb_array_elements_text(track_ids) AS elem
				WHERE NOT (elem = ANY($1::text[]))
			) WHERE track_ids ?| $1`, pq.Array(deletedIDs))
		e.db.ExecContext(ctx,
			`UPDATE jukeboxes SET
				queue = COALESCE(
					(SELECT jsonb_agg(elem) FROM jsonb_array_elements_text(queue) AS elem
					 WHERE NOT (elem = ANY($1::text[]))),
					'[]'::jsonb
				),
				queue_idx = 0,
				shuffle_order = '[]'::jsonb,
				shuffle_idx = 0`, pq.Array(deletedIDs))
	}

	e.db.ExecContext(ctx, `DELETE FROM favorites WHERE item_type = 'album' AND item_id NOT IN (SELECT DISTINCT album_id FROM tracks)`)
	e.db.ExecContext(ctx, `DELETE FROM albums WHERE id NOT IN (SELECT DISTINCT album_id FROM tracks)`)
	e.db.ExecContext(ctx, `DELETE FROM favorites WHERE item_type = 'artist' AND item_id NOT IN (SELECT DISTINCT artist_id FROM tracks)`)
	e.db.ExecContext(ctx, `DELETE FROM artists WHERE id NOT IN (SELECT DISTINCT artist_id FROM tracks)`)

	lib.TrackCount = len(existingByPath) + stats.NewTracks - stats.DeletedTracks
	lib.LastScannedAt = timePtr(time.Now())

	log.Printf("[scan] library=%s total=%d new=%d updated=%d deleted=%d covers=%d errors=%d",
		lib.Name, stats.TotalFiles, stats.NewTracks, stats.UpdatedTracks, stats.DeletedTracks, stats.CoversExtracted, len(stats.Errors))

	return stats, nil
}

func (e *Engine) ExtractCovers(ctx context.Context, lib *domain.Library, onProgress func(scanned, total int)) error {
	tracks, err := e.trackRepo.FindByLibraryID(ctx, lib.ID)
	if err != nil {
		return fmt.Errorf("failed to load tracks: %w", err)
	}

	total := len(tracks)
	for i, t := range tracks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if thumbnailExists(e.coverExtractor, lib.ID, t.ID, 64) {
			continue
		}

		data, _, err := e.coverExtractor.ExtractFromFile(t.FilePath)
		if err != nil {
			continue
		}
		thumbDir := filepath.Join(e.coverExtractor.ImagesDir(), lib.ID)
		os.MkdirAll(thumbDir, 0755)
		thumbPath := filepath.Join(thumbDir, fmt.Sprintf("track_%s_64.jpg", t.ID))
		metadata.ResizeToThumbnail(data, thumbPath, 64)

		if onProgress != nil {
			onProgress(i+1, total)
		}
	}
	return nil
}

func mainCoverExists(ce *metadata.CoverExtractor, libraryID, trackID string) bool {
	p := filepath.Join(ce.ImagesDir(), libraryID, fmt.Sprintf("track_%s.jpg", trackID))
	_, err := os.Stat(p)
	return err == nil
}

func mainAlbumCoverExists(ce *metadata.CoverExtractor, albumID string) bool {
	p := filepath.Join(ce.ImagesDir(), "album", fmt.Sprintf("album_%s.jpg", albumID))
	_, err := os.Stat(p)
	return err == nil
}

func thumbnailExists(ce *metadata.CoverExtractor, libraryID, trackID string, size int) bool {
	p := filepath.Join(ce.ImagesDir(), libraryID, fmt.Sprintf("track_%s_%d.jpg", trackID, size))
	_, err := os.Stat(p)
	return err == nil
}

func (e *Engine) ensureThumbnail(libraryID, trackID string) error {
	mainPath := filepath.Join(e.coverExtractor.ImagesDir(), libraryID, fmt.Sprintf("track_%s.jpg", trackID))
	data, err := os.ReadFile(mainPath)
	if err != nil {
		return fmt.Errorf("main cover not found for track %s", trackID)
	}
	for _, size := range []int{64, 256} {
		thumbPath := filepath.Join(e.coverExtractor.ImagesDir(), libraryID, fmt.Sprintf("track_%s_%d.jpg", trackID, size))
		if _, err := os.Stat(thumbPath); err == nil {
			continue
		}
		metadata.ResizeToThumbnail(data, thumbPath, size)
	}
	return nil
}

func (e *Engine) metaComplete(ctx context.Context, track *domain.Track) bool {
	if track == nil {
		return false
	}
	if track.MBID == "" {
		return false
	}
	if track.ArtistID != "" {
		artist, err := e.artistRepo.FindByID(ctx, track.ArtistID)
		if err != nil || artist.Name == "" || artist.Name == "Unknown Artist" ||
			artist.MBID == "" || artist.Country == "" {
			return false
		}
	}
	if track.AlbumID != "" {
		album, err := e.albumRepo.FindByID(ctx, track.AlbumID)
		if err != nil || album.Title == "" || album.Title == "Unknown Album" ||
			album.MBID == "" || album.Country == "" ||
			album.Year == 0 || album.Genre == "" {
			return false
		}
	}
	return true
}

func (e *Engine) findOrCreateArtist(ctx context.Context, libraryID, name string, enrichment *metadata.EnrichmentResult) (*domain.Artist, error) {
	if name == "" {
		name = "Unknown Artist"
	}

	if enrichment != nil && enrichment.ArtistMBID != "" {
		if artist, err := e.artistRepo.FindByMBID(ctx, enrichment.ArtistMBID); err == nil {
			if artist.Name == "" && enrichment.Artist != "" {
				artist.Name = enrichment.Artist
				artist.SortName = enrichment.Artist
				e.artistRepo.Update(ctx, artist)
			}
			if artist.Country == "" && enrichment.ArtistCountry != "" {
				artist.Country = enrichment.ArtistCountry
				e.artistRepo.Update(ctx, artist)
			}
			return artist, nil
		}
	}

	artist, err := e.artistRepo.FindByName(ctx, name)
	if err == nil {
		if artist.MBID == "" && enrichment != nil && enrichment.ArtistMBID != "" {
			artist.MBID = enrichment.ArtistMBID
			e.artistRepo.Update(ctx, artist)
		}
		if artist.Country == "" && enrichment != nil && enrichment.ArtistCountry != "" {
			artist.Country = enrichment.ArtistCountry
			e.artistRepo.Update(ctx, artist)
		}
		return artist, nil
	}

	now := time.Now()
	artist = &domain.Artist{
		ID:        domain.NewID(),
		Name:      name,
		SortName:  name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if enrichment != nil {
		artist.MBID = enrichment.ArtistMBID
		artist.Country = enrichment.ArtistCountry
	}

	err = e.artistRepo.BatchCreate(ctx, []domain.Artist{*artist})
	if err != nil {
		return nil, err
	}

	return artist, nil
}

func (e *Engine) findOrCreateAlbum(ctx context.Context, libraryID, title, artistID string, year int, genre string, enrichment *metadata.EnrichmentResult) (*domain.Album, error) {
	if title == "" {
		title = "Unknown Album"
	}

	if enrichment != nil && enrichment.AlbumMBID != "" {
		if album, err := e.albumRepo.FindByMBID(ctx, enrichment.AlbumMBID); err == nil {
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
				e.albumRepo.Update(ctx, album)
			}
			return album, nil
		}
	}

	album, err := e.albumRepo.FindByTitleAndArtist(ctx, title, artistID)
	if err == nil {
		updated := false
		if album.MBID == "" && enrichment != nil && enrichment.AlbumMBID != "" {
			album.MBID = enrichment.AlbumMBID
			updated = true
		}
		if enrichment != nil && album.Year == 0 && enrichment.Year != 0 {
			album.Year = enrichment.Year
			updated = true
		}
		if enrichment != nil && album.Genre == "" && enrichment.Genre != "" {
			album.Genre = enrichment.Genre
			updated = true
		}
		if enrichment != nil && album.Country == "" && enrichment.AlbumCountry != "" {
			album.Country = enrichment.AlbumCountry
			updated = true
		}
		if updated {
			e.albumRepo.Update(ctx, album)
		}
		return album, nil
	}

	now := time.Now()
	album = &domain.Album{
		ID:        domain.NewID(),
		Title:     title,
		ArtistID:  artistID,
		Year:      year,
		Genre:     genre,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if enrichment != nil {
		album.MBID = enrichment.AlbumMBID
		album.Country = enrichment.AlbumCountry
		if year == 0 && enrichment.Year != 0 {
			album.Year = enrichment.Year
		}
		if genre == "" && enrichment.Genre != "" {
			album.Genre = enrichment.Genre
		}
	}

	err = e.albumRepo.BatchCreate(ctx, []domain.Album{*album})
	if err != nil {
		return nil, err
	}

	return album, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func timePtr(t time.Time) *time.Time {
	return &t
}
