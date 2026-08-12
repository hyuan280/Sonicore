package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/internal/infrastructure/scanner"
)

type ScanProgress struct {
	LibraryID     string `json:"library_id"`
	Status        string `json:"status"`
	TotalFiles    int    `json:"total_files"`
	Scanned       int    `json:"scanned"`
	NewTracks     int    `json:"new_tracks"`
	UpdatedTracks int    `json:"updated_tracks"`
	DeletedTracks int    `json:"deleted_tracks"`
	Errors        int    `json:"errors"`
}

type ScannerService struct {
	db         *sql.DB
	engine     *scanner.Engine
	scanRepo   *repository.ScanJobRepo
	libRepo    *repository.LibraryRepo
	settingsRepo *repository.SettingsRepo
	imagesDir  string
	lyricsDir  string
	mbCfg      metadata.MBConfig

	mu         sync.RWMutex
	activeScan map[string]*ScanProgress
}

func NewScannerService(db *sql.DB, imagesDir, lyricsDir string, mbCfg metadata.MBConfig) *ScannerService {
	return &ScannerService{
		db:           db,
		engine:       scanner.NewEngine(db, imagesDir, mbCfg, lyricsDir),
		scanRepo:     repository.NewScanJobRepo(db),
		libRepo:      repository.NewLibraryRepo(db),
		settingsRepo: repository.NewSettingsRepo(db),
		imagesDir:    imagesDir,
		lyricsDir:    lyricsDir,
		mbCfg:       mbCfg,
		activeScan:   make(map[string]*ScanProgress),
	}
}

// rebuildEngine re-creates the scanner engine, reading latest MB config from DB if available.
func (s *ScannerService) rebuildEngine(ctx context.Context) {
	cfg := s.mbCfg
	if enabled, err := s.settingsRepo.Get(ctx, "metadata_musicbrainz_enabled"); err == nil {
		cfg.Enabled = enabled == "true"
	}
	if url, err := s.settingsRepo.Get(ctx, "metadata_musicbrainz_api_url"); err == nil && url != "" {
		cfg.APIURL = url
	}
	if rl, err := s.settingsRepo.Get(ctx, "metadata_musicbrainz_rate_limit"); err == nil && rl != "" {
		fmt.Sscanf(rl, "%d", &cfg.RateLimit)
	}
	s.engine = scanner.NewEngine(s.db, s.imagesDir, cfg, s.lyricsDir)
}

func (s *ScannerService) GetProgress(libraryID string) *ScanProgress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeScan[libraryID]
}

func (s *ScannerService) StartScan(ctx context.Context, libraryID string, mode string) error {
	s.mu.Lock()
	if _, running := s.activeScan[libraryID]; running {
		s.mu.Unlock()
		return fmt.Errorf("scan already running for library %s", libraryID)
	}
	s.rebuildEngine(ctx)
	s.activeScan[libraryID] = &ScanProgress{
		LibraryID: libraryID,
		Status:    "running",
	}
	s.mu.Unlock()

	if mode != "overwrite" {
		mode = "missing"
	}
	go s.runScan(context.Background(), libraryID, mode)
	return nil
}

func (s *ScannerService) runScan(ctx context.Context, libraryID, mode string) {
	lib, err := s.libRepo.FindByID(ctx, libraryID)
	if err != nil {
		s.setError(libraryID, fmt.Sprintf("library not found: %v", err))
		return
	}

	now := time.Now()
	job := &domain.ScanJob{
		ID:        domain.NewID(),
		LibraryID: libraryID,
		Type:      "full",
		Status:    "running",
		CreatedAt: now,
	}
	s.scanRepo.Create(ctx, job)

	stats, err := s.engine.ScanLibrary(ctx, lib, scanner.ScanOptions{Mode: mode}, func(stats scanner.ScanStats) {
		s.mu.Lock()
		if p := s.activeScan[libraryID]; p != nil {
			p.TotalFiles = stats.TotalFiles
			p.Scanned = stats.Scanned
			p.NewTracks = stats.NewTracks
			p.UpdatedTracks = stats.UpdatedTracks
			p.DeletedTracks = stats.DeletedTracks
			p.Errors = len(stats.Errors)
		}
		s.mu.Unlock()
	})
	// ScanLibrary may return (nil, err) on DB failure — never dereference nil stats.
	if stats == nil {
		stats = &scanner.ScanStats{}
	}

	s.mu.Lock()
	if p := s.activeScan[libraryID]; p != nil {
		if err != nil {
			p.Status = "failed"
		} else {
			p.Status = "completed"
		}
	}
	s.mu.Unlock()

	completedAt := time.Now()
	job.Status = "completed"
	job.TotalFiles = stats.TotalFiles
	job.Scanned = stats.Scanned
	job.NewTracks = stats.NewTracks
	job.UpdatedTracks = stats.UpdatedTracks
	job.DeletedTracks = stats.DeletedTracks
	job.CompletedAt = &completedAt
	if len(stats.Errors) > 0 {
		errData, _ := json.Marshal(stats.Errors)
		job.Errors = string(errData)
	}
	s.scanRepo.Update(ctx, job)

	lib.LastScanErrors = len(stats.Errors)
	lib.UpdatedAt = time.Now()
	s.libRepo.UpdateStats(ctx, lib)

	log.Printf("[scanner] finished library=%s status=%s new=%d updated=%d deleted=%d errors=%d",
		libraryID, job.Status, job.NewTracks, job.UpdatedTracks, job.DeletedTracks, len(stats.Errors))

	s.mu.Lock()
	delete(s.activeScan, libraryID)
	s.mu.Unlock()
}

func (s *ScannerService) setError(libraryID, msg string) {
	s.mu.Lock()
	if p := s.activeScan[libraryID]; p != nil {
		p.Status = "failed"
	}
	s.mu.Unlock()
	s.mu.Lock()
	delete(s.activeScan, libraryID)
	s.mu.Unlock()
	log.Printf("[scanner] error library=%s: %s", libraryID, msg)
}
