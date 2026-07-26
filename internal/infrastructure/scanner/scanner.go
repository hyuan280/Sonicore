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
	db       *sql.DB
	trackRepo  *repository.TrackRepo
	albumRepo  *repository.AlbumRepo
	artistRepo *repository.ArtistRepo
}

func NewEngine(db *sql.DB) *Engine {
	return &Engine{
		db:       db,
		trackRepo:  repository.NewTrackRepo(db),
		albumRepo:  repository.NewAlbumRepo(db),
		artistRepo: repository.NewArtistRepo(db),
	}
}

type ScanStats struct {
	TotalFiles    int
	Scanned       int
	NewTracks     int
	UpdatedTracks int
	DeletedTracks int
	Errors        []string
}

func (e *Engine) ScanLibrary(ctx context.Context, lib *domain.Library, onProgress func(stats ScanStats)) (*ScanStats, error) {
	stats := &ScanStats{}

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

		stats.TotalFiles++

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

		seenPaths[path] = true

		if existing, ok := existingByPath[path]; ok {
			if existing.Hash == fileHash {
				stats.Scanned++
				onProgress(*stats)
				return nil
			}
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
		track := &domain.Track{
			ID:          domain.NewID(),
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

		if existing, ok := existingByPath[path]; ok {
			track.ID = existing.ID
			track.CreatedAt = existing.CreatedAt
			track.Rating = existing.Rating
			track.PlayCount = existing.PlayCount
			track.LastPlayedAt = existing.LastPlayedAt
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

	for path := range existingByPath {
		if !seenPaths[path] {
			if err := e.trackRepo.DeleteByFilePath(ctx, path, lib.ID); err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("delete error %s: %v", path, err))
			} else {
				stats.DeletedTracks++
			}
		}
	}

	lib.TrackCount = len(existingByPath) + stats.NewTracks - stats.DeletedTracks
	lib.LastScannedAt = timePtr(time.Now())

	log.Printf("[scan] library=%s total=%d new=%d updated=%d deleted=%d errors=%d",
		lib.Name, stats.TotalFiles, stats.NewTracks, stats.UpdatedTracks, stats.DeletedTracks, len(stats.Errors))

	return stats, nil
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
