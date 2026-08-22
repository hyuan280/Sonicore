package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/external/netease"
	"github.com/sonicore/server/internal/infrastructure/logger"
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
	db              *sql.DB
	engine          *scanner.Engine
	scanRepo        *repository.ScanJobRepo
	libRepo         *repository.LibraryRepo
	settingsRepo    *repository.SettingsRepo
	umRepo          *repository.UserMetadataRepo
	imagesDir       string
	lyricsDir       string
	mbCfg           metadata.MBConfig
	mbClient        *metadata.MBClient
	neteaseProvider *netease.Provider
	neteaseEnabled  bool
	covers          *metadata.CoverManager

	mu         sync.RWMutex
	activeScan map[string]*ScanProgress

	// registryMu guards the TTL cache for buildRegistry.
	registryMu  sync.Mutex
	registryVal *metadata.Registry
	registryAt  time.Time
	// registryGen increments on every explicit rebuild (rebuildEngine) so an
	// in-flight build started with the old settings cannot repopulate the
	// cache after an invalidation.
	registryGen uint64
	// registryFlight shares one in-flight registry build across concurrent
	// callers (single-flight), so a cache miss does not make every cover
	// lookup rebuild the source chain independently.
	registryFlight *registryFlight
	// lastGoodRegistry is the most recent registry assembled from fully
	// successful settings reads. When a read fails (DB blip, cancelled scan
	// context), buildRegistryUnlocked returns it instead of publishing a
	// default-config chain into the TTL cache. Atomic because rebuildEngine
	// can clear the flight slot and let a new-generation builder run
	// concurrently with a still-executing old one.
	lastGoodRegistry atomic.Pointer[metadata.Registry]
}

// registryFlight tracks one in-flight registry build so concurrent callers
// wait for the same build instead of each reading settings and rebuilding.
type registryFlight struct {
	gen  uint64
	done chan struct{}
	reg  *metadata.Registry
}

// NewScannerService builds the scanner service. neteaseProvider is shared
// with the platform handlers and powers the NetEase metadata source when
// neteaseEnabled is true. covers is the shared cover manager (may be nil; a
// private one is created then — note that sharing is required to serialize
// extraction across scanner and HTTP paths).
func NewScannerService(db *sql.DB, imagesDir, lyricsDir string, mbCfg metadata.MBConfig, mbClient *metadata.MBClient, neteaseProvider *netease.Provider, neteaseEnabled bool, covers *metadata.CoverManager) *ScannerService {
	s := &ScannerService{
		db:              db,
		scanRepo:        repository.NewScanJobRepo(db),
		libRepo:         repository.NewLibraryRepo(db),
		settingsRepo:    repository.NewSettingsRepo(db),
		umRepo:          repository.NewUserMetadataRepo(db),
		imagesDir:       imagesDir,
		lyricsDir:       lyricsDir,
		mbCfg:           mbCfg,
		mbClient:        mbClient,
		neteaseProvider: neteaseProvider,
		neteaseEnabled:  neteaseEnabled,
		activeScan:      make(map[string]*ScanProgress),
	}
	if covers == nil {
		covers = metadata.NewCoverManager(imagesDir, db, func() *metadata.Registry { return s.buildRegistry(context.Background()) })
	}
	s.covers = covers
	s.engine = scanner.NewEngine(db, imagesDir, s.buildRegistry(context.Background()), lyricsDir, covers, s.umRepo)
	return s
}

// buildRegistry assembles the enabled metadata sources in priority order
// from the latest settings. MusicBrainz is the primary source; NetEase is
// the fallback (requires both the metadata switch and a platform provider).
// Results are cached for a short TTL so the engine's per-track cover lookups
// do not re-read settings and rebuild the source chain on every track.
func (s *ScannerService) buildRegistry(ctx context.Context) *metadata.Registry {
	s.registryMu.Lock()
	if s.registryVal != nil && time.Since(s.registryAt) < settingsCacheTTL {
		reg := s.registryVal
		s.registryMu.Unlock()
		return reg
	}
	gen := s.registryGen
	// Single-flight: join an in-flight build for the same generation instead
	// of running the (slow) settings reads and source-chain assembly again.
	if fl := s.registryFlight; fl != nil && fl.gen == gen {
		s.registryMu.Unlock()
		<-fl.done
		// A panicked builder closes done without publishing a registry (the
		// defer above only clears the slot). Handing out nil would crash the
		// cover chain or silently disable enrichment, so re-enter the cache
		// check / single-flight instead — a newer build may have published.
		if fl.reg == nil {
			return s.buildRegistry(ctx)
		}
		return fl.reg
	}
	fl := &registryFlight{gen: gen, done: make(chan struct{})}
	s.registryFlight = fl
	s.registryMu.Unlock()

	// Release the flight on every exit — including a panic in the build below:
	// a stuck flight would leave every same-generation caller blocked forever
	// on <-fl.done. The slot is only cleared when we still own it (a
	// rebuildEngine may have replaced it with a newer generation's flight).
	defer func() {
		s.registryMu.Lock()
		if s.registryFlight == fl {
			s.registryFlight = nil
		}
		if fl.reg == nil {
			select {
			case <-fl.done:
			default:
				close(fl.done)
			}
		}
		s.registryMu.Unlock()
	}()

	// Build outside the lock: the settings reads, source-chain assembly and
	// logging below must not serialize every concurrent caller (cover
	// lookups, engine rebuilds) behind a slow or failing DB. readOK reports
	// whether every settings read succeeded; cacheable reports whether the
	// assembled registry may be published to the TTL cache (false only for a
	// degraded default-config build after a read failure with no last-good
	// fallback).
	registry, readOK, cacheable := s.buildRegistryUnlocked(ctx)

	s.registryMu.Lock()
	fl.reg = registry
	close(fl.done)
	// Only clear the slot when we still occupy it: an unconditional reset
	// would clobber a newer flight created after a rebuildEngine bumped the
	// generation, silently disabling single-flight for the new callers.
	if s.registryFlight == fl {
		s.registryFlight = nil
	}
	// Only publish when no rebuildEngine invalidated the cache while we were
	// building — a stale build started before an explicit rebuild must not
	// repopulate the cache (and thereby serve stale settings for the TTL).
	// A degraded default-config build is never cached: publishing it would
	// make the registry ignore admin settings for the TTL window.
	if s.registryGen == gen && cacheable {
		s.registryVal = registry
		s.registryAt = time.Now()
		// lastGoodRegistry is only refreshed from a build that actually read
		// the settings AND still matches the current generation: a stale-gen
		// builder finishing late must not overwrite the newer config.
		if readOK {
			s.lastGoodRegistry.Store(registry)
		}
	}
	s.registryMu.Unlock()
	return registry
}

// buildRegistryUnlocked assembles the enabled metadata sources in priority
// order from the latest settings. MusicBrainz is the primary source; NetEase
// is the fallback (requires both the metadata switch and a platform
// provider). Caller must not hold registryMu.
//
// readOK reports whether every settings read succeeded. cacheable reports
// whether the returned registry may be published to the TTL cache: it is
// true for a fresh successful build and for a lastGoodRegistry fallback (a
// valid previous config), but false for a degraded default-config build (a
// read failure with no last-good registry to fall back to) — the caller must
// not cache that.
func (s *ScannerService) buildRegistryUnlocked(ctx context.Context) (*metadata.Registry, bool, bool) {
	mbCfg := s.mbCfg
	mbCfg.Client = s.mbClient
	// A failed settings read must not be conflated with "not set": publishing
	// a default-config chain would silently ignore the admin's saved
	// switches/URLs/rate limits for the whole scan. Any read error therefore
	// falls back to the last registry assembled from successful reads.
	readErr := false
	var firstErr error
	var firstKey string
	read := func(key string) (string, error) {
		v, err := s.settingsRepo.Get(ctx, key)
		if err != nil && !readErr {
			firstErr, firstKey = err, key
			readErr = true
		}
		return v, err
	}
	// Get returns ("", nil) for missing keys; only override when a value
	// is actually stored.
	if enabled, err := read("metadata_musicbrainz_enabled"); err == nil && enabled != "" {
		mbCfg.Enabled = enabled == "true"
	}
	if url, err := read("metadata_musicbrainz_api_url"); err == nil && url != "" {
		mbCfg.APIURL = url
	}
	if rl, err := read("metadata_musicbrainz_rate_limit"); err == nil && rl != "" {
		if n, err := strconv.Atoi(rl); err != nil || n <= 0 {
			logger.Warn("[scanner] invalid musicbrainz rate limit %q", rl)
		} else {
			mbCfg.RateLimit = n
		}
	}

	neteaseEnabled := s.neteaseEnabled
	// Get returns ("", nil) for missing keys; only override when a value
	// is actually stored.
	if enabled, err := read("metadata_netease_enabled"); err == nil && enabled != "" {
		neteaseEnabled = enabled == "true"
	}

	if readErr {
		logger.Error("[scanner] settings read failed for %q: %v", firstKey, firstErr)
		if last := s.lastGoodRegistry.Load(); last != nil {
			return last, false, true
		}
		logger.Info("[scanner] no last-good registry, falling back to defaults")
	}

	var sources []port.MetadataSource
	sources = append(sources, metadata.NewMBSource(mbCfg))
	if neteaseEnabled && s.neteaseProvider != nil {
		sources = append(sources, metadata.NewNeteaseSource(s.neteaseProvider, true))
	}
	sources = append(sources, metadata.NewUserSource(s.umRepo))
	registry := metadata.BuildRegistry(sources...)
	if names := sourceNames(registry.Sources()); len(names) > 0 {
		logger.Info("[scanner] metadata sources: %s", strings.Join(names, ", "))
	}
	// lastGoodRegistry is refreshed by the caller (buildRegistry) under its
	// generation check, so a stale-gen build never overwrites new config.
	return registry, !readErr, !readErr
}

// settingsCacheTTL bounds how stale a rebuilt metadata registry may be.
const settingsCacheTTL = 5 * time.Second

// sourceNames extracts source names for logging.
func sourceNames(sources []port.MetadataSource) []string {
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.Name())
	}
	return names
}

// rebuildEngine re-creates the scanner engine, rebuilding the registry
// unconditionally (bypassing the TTL cache) so an admin settings change is
// picked up by the next scan instead of being pinned to the cached (possibly
// stale) source chain.
func (s *ScannerService) rebuildEngine(ctx context.Context) {
	s.registryMu.Lock()
	s.registryGen++
	s.registryVal = nil
	// An in-flight build for the previous generation must not be joined by
	// callers after the rebuild; it finishes and publishes nothing (gen
	// mismatch) while new callers start a fresh flight.
	s.registryFlight = nil
	s.registryMu.Unlock()
	s.engine = scanner.NewEngine(s.db, s.imagesDir, s.buildRegistry(ctx), s.lyricsDir, s.covers, s.umRepo)
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
	// Snapshot the engine while still holding the lock: rebuildEngine just
	// swapped it, and the scan goroutine must run against the engine it was
	// started with (concurrent scans of other libraries may rebuild it again
	// meanwhile).
	engine := s.engine
	s.mu.Unlock()

	if mode != "overwrite" {
		mode = "missing"
	}
	go s.runScan(context.Background(), libraryID, mode, engine)
	return nil
}

func (s *ScannerService) runScan(ctx context.Context, libraryID, mode string, engine *scanner.Engine) {
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

	stats, err := engine.ScanLibrary(ctx, lib, scanner.ScanOptions{Mode: mode}, func(stats scanner.ScanStats) {
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

	logger.Info("[scanner] finished library=%s status=%s new=%d updated=%d deleted=%d errors=%d",
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
	logger.Error("[scanner] error library=%s: %s", libraryID, msg)
}
