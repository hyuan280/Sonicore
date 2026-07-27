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

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type Engine struct {
	db             *sql.DB
	trackRepo      *repository.TrackRepo
	albumRepo      *repository.AlbumRepo
	artistRepo     *repository.ArtistRepo
	coverExtractor *metadata.CoverExtractor
}

func NewEngine(db *sql.DB, imagesDir string) *Engine {
	return &Engine{
		db:             db,
		trackRepo:      repository.NewTrackRepo(db),
		albumRepo:      repository.NewAlbumRepo(db),
		artistRepo:     repository.NewArtistRepo(db),
		coverExtractor: metadata.NewCoverExtractor(imagesDir),
	}
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

func (e *Engine) ScanLibrary(ctx context.Context, lib *domain.Library, onProgress func(stats ScanStats)) (*ScanStats, error) {
	stats := &ScanStats{}

	// 1. pre-count: walk to get total files upfront (for progress bar)
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

		// 2. read tags (ffprobe)
		meta, err := metadata.Probe(path)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("probe error %s: %v", path, err))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		// 3. SHA256 + DB dedup
		fileHash, err := hashFile(path)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("hash error %s: %v", path, err))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		seenPaths[path] = true

		var existing *domain.Track
		if e, ok := existingByPath[path]; ok {
			existing = e
		}

		// hash unchanged → skip DB update, but still try cover extraction + thumbnail
		if existing != nil && existing.Hash == fileHash {
			if meta.HasCoverArt {
				if !mainCoverExists(e.coverExtractor, lib.ID, existing.ID) {
					if data, contentType, err := e.coverExtractor.ExtractFromFile(path); err == nil {
						ext := "jpg"
						if contentType == "image/png" {
							ext = "png"
						}
						if _, err := e.coverExtractor.Save(lib.ID, "track", existing.ID, data, ext); err == nil {
							if existing.CoverImageID == nil {
								existing.CoverImageID = &existing.ID
								e.trackRepo.Update(ctx, existing)
							}
							stats.CoversExtracted++
						}
					}
				} else if !thumbnailExists(e.coverExtractor, lib.ID, existing.ID) {
					if err := e.ensureThumbnail(lib.ID, existing.ID); err == nil {
						stats.CoversExtracted++
					}
				}
			}
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		artistName := meta.Artist
		if artistName == "" {
			artistName = "Unknown Artist"
		}
		albumName := meta.Album
		if albumName == "" {
			albumName = "Unknown Album"
		}

		artist, err := e.findOrCreateArtist(ctx, lib.ID, artistName)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("artist error: %v", err))
			stats.Scanned++
			onProgress(*stats)
			return nil
		}

		album, err := e.findOrCreateAlbum(ctx, lib.ID, albumName, artist.ID, meta.Year, meta.Genre)
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
			Title:       meta.Title,
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

		if existing != nil {
			track.CreatedAt = existing.CreatedAt
			track.Rating = existing.Rating
			track.PlayCount = existing.PlayCount
			track.LastPlayedAt = existing.LastPlayedAt
			track.CoverImageID = existing.CoverImageID
		}

		// extract cover + thumbnail
		if meta.HasCoverArt {
			if !mainCoverExists(e.coverExtractor, lib.ID, trackID) {
				if data, contentType, err := e.coverExtractor.ExtractFromFile(path); err == nil {
					ext := "jpg"
					if contentType == "image/png" {
						ext = "png"
					}
					if _, err := e.coverExtractor.Save(lib.ID, "track", trackID, data, ext); err == nil {
						track.CoverImageID = &trackID
						stats.CoversExtracted++
					}
				}
			} else if !thumbnailExists(e.coverExtractor, lib.ID, trackID) {
				if err := e.ensureThumbnail(lib.ID, trackID); err == nil {
					stats.CoversExtracted++
				}
			}
		}

		// 4. update DB
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

	// 5. remove deleted tracks
	for path := range existingByPath {
		if !seenPaths[path] {
			if err := e.trackRepo.DeleteByFilePath(ctx, path, lib.ID); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("delete error %s: %v", path, err))
			} else {
				stats.DeletedTracks++
			}
		}
	}

	// 6. update final stats
	lib.TrackCount = len(existingByPath) + stats.NewTracks - stats.DeletedTracks
	lib.LastScannedAt = timePtr(time.Now())

	log.Printf("[scan] library=%s total=%d new=%d updated=%d deleted=%d covers=%d errors=%d",
		lib.Name, stats.TotalFiles, stats.NewTracks, stats.UpdatedTracks, stats.DeletedTracks, stats.CoversExtracted, len(stats.Errors))

	return stats, nil
}

// ExtractCovers walks all tracks in a library and extracts embedded cover art
// when no cover file exists on disk yet. Designed for periodic/incremental use.
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

		// skip if main cover + thumbnail already exist
		if mainCoverExists(e.coverExtractor, lib.ID, t.ID) && thumbnailExists(e.coverExtractor, lib.ID, t.ID) {
			continue
		}
		// ensure thumbnail if main exists but thumbnail missing
		if mainCoverExists(e.coverExtractor, lib.ID, t.ID) {
			e.ensureThumbnail(lib.ID, t.ID)
			continue
		}

		data, contentType, err := e.coverExtractor.ExtractFromFile(t.FilePath)
		if err != nil {
			continue
		}
		ext := "jpg"
		if contentType == "image/png" {
			ext = "png"
		}
		if _, err := e.coverExtractor.Save(lib.ID, "track", t.ID, data, ext); err != nil {
			continue
		}

		if onProgress != nil {
			onProgress(i+1, total)
		}
	}
	return nil
}

func mainCoverExists(ce *metadata.CoverExtractor, libraryID, trackID string) bool {
	for _, ext := range []string{"jpg", "png"} {
		p := filepath.Join(ce.ImagesDir(), libraryID, fmt.Sprintf("track_%s.%s", trackID, ext))
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func thumbnailExists(ce *metadata.CoverExtractor, libraryID, trackID string) bool {
	p := filepath.Join(ce.ImagesDir(), libraryID, fmt.Sprintf("track_%s_256.jpg", trackID))
	_, err := os.Stat(p)
	return err == nil
}

func (e *Engine) ensureThumbnail(libraryID, trackID string) error {
	for _, ext := range []string{"jpg", "png"} {
		mainPath := filepath.Join(e.coverExtractor.ImagesDir(), libraryID, fmt.Sprintf("track_%s.%s", trackID, ext))
		thumbPath := filepath.Join(e.coverExtractor.ImagesDir(), libraryID, fmt.Sprintf("track_%s_256.jpg", trackID))
		if data, err := os.ReadFile(mainPath); err == nil {
			metadata.ResizeToThumbnail(data, thumbPath, 256)
			if _, err := os.Stat(thumbPath); err == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("failed to create thumbnail for track %s", trackID)
}

func (e *Engine) findOrCreateArtist(ctx context.Context, libraryID, name string) (*domain.Artist, error) {
	artist, err := e.artistRepo.FindByNameAndLibrary(ctx, name, libraryID)
	if err == nil {
		return artist, nil
	}

	now := time.Now()
	artist = &domain.Artist{
		ID:        domain.NewID(),
		LibraryID: libraryID,
		Name:      name,
		SortName:  name,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err = e.artistRepo.BatchCreate(ctx, []domain.Artist{*artist})
	if err != nil {
		return nil, err
	}

	return artist, nil
}

func (e *Engine) findOrCreateAlbum(ctx context.Context, libraryID, title, artistID string, year int, genre string) (*domain.Album, error) {
	album, err := e.albumRepo.FindByNameAndArtist(ctx, title, artistID, libraryID)
	if err == nil {
		return album, nil
	}

	now := time.Now()
	album = &domain.Album{
		ID:        domain.NewID(),
		LibraryID: libraryID,
		Title:     title,
		ArtistID:  artistID,
		Year:      year,
		Genre:     genre,
		CreatedAt: now,
		UpdatedAt: now,
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
