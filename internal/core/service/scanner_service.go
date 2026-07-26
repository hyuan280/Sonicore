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
	db       *sql.DB
	engine   *scanner.Engine
	scanRepo *repository.ScanJobRepo
	libRepo  *repository.LibraryRepo

	mu         sync.RWMutex
	activeScan map[string]*ScanProgress
}

func NewScannerService(db *sql.DB) *ScannerService {
	return &ScannerService{
		db:         db,
		engine:     scanner.NewEngine(db),
		scanRepo:   repository.NewScanJobRepo(db),
		libRepo:    repository.NewLibraryRepo(db),
		activeScan: make(map[string]*ScanProgress),
	}
}

func (s *ScannerService) GetProgress(libraryID string) *ScanProgress {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeScan[libraryID]
}

func (s *ScannerService) StartScan(ctx context.Context, libraryID string) error {
	s.mu.Lock()
	if _, running := s.activeScan[libraryID]; running {
		s.mu.Unlock()
		return fmt.Errorf("scan already running for library %s", libraryID)
	}
	s.activeScan[libraryID] = &ScanProgress{
		LibraryID: libraryID,
		Status:    "running",
	}
	s.mu.Unlock()

	go s.runScan(context.Background(), libraryID)
	return nil
}

func (s *ScannerService) runScan(ctx context.Context, libraryID string) {
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

	stats, err := s.engine.ScanLibrary(ctx, lib, func(stats scanner.ScanStats) {
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

	lib.UpdatedAt = time.Now()
	s.libRepo.UpdateStats(ctx, lib)

	s.db.ExecContext(ctx,
		`UPDATE artists SET album_count = (SELECT COUNT(*) FROM albums WHERE albums.artist_id = artists.id)
		 WHERE library_id = $1`, libraryID)

	log.Printf("[scanner] finished library=%s status=%s new=%d updated=%d deleted=%d errors=%d",
		libraryID, job.Status, job.NewTracks, job.UpdatedTracks, job.DeletedTracks, len(stats.Errors))

	time.AfterFunc(30*time.Second, func() {
		s.mu.Lock()
		delete(s.activeScan, libraryID)
		s.mu.Unlock()
	})
}

func (s *ScannerService) setError(libraryID, msg string) {
	s.mu.Lock()
	if p := s.activeScan[libraryID]; p != nil {
		p.Status = "failed"
	}
	s.mu.Unlock()
	log.Printf("[scanner] error library=%s: %s", libraryID, msg)
}
