package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// scanRun is one in-flight scan's bookkeeping. Pointer identity is used so a
// finishing goroutine clears only its own entry: after CancelScan removes the
// entry, a concurrent StartScan may install a new run for the same library and
// the old goroutine must not delete it.
type scanRun struct {
	progress *ScanProgress
	cancel   context.CancelFunc
	// done is closed when runScan returns, so CancelScan can wait for the scan
	// to actually stop before the caller tears down the library's files.
	done chan struct{}
}

// scanCancelWait bounds how long CancelScan waits for a cancelled scan to
// unwind (it checks the context at each file boundary and ffmpeg/metadata
// calls honor it). A var so tests can shorten it.
var scanCancelWait = 10 * time.Second

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
	notif           *NotificationService

	mu sync.RWMutex
	// activeScan tracks each running scan by library id. The value carries the
	// scan's own cancel func and completion signal so it can be stopped and so
	// a finishing scan only clears its OWN entry (a later scan may have
	// replaced it after a CancelScan).
	activeScan map[string]*scanRun
	// deleting marks libraries whose deletion is in progress so no scan can be
	// started for them while teardown runs (otherwise a new scan could recreate
	// the directories the delete is removing, or write rows for a gone library).
	deleting map[string]bool

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
func NewScannerService(db *sql.DB, imagesDir, lyricsDir string, mbCfg metadata.MBConfig, mbClient *metadata.MBClient, neteaseProvider *netease.Provider, neteaseEnabled bool, covers *metadata.CoverManager, notif *NotificationService) *ScannerService {
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
		notif:           notif,
		activeScan:      make(map[string]*scanRun),
		deleting:        make(map[string]bool),
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
	read := func(name string) (string, error) {
		v, err := s.settingsRepo.Get(ctx, repository.CategorySource, name)
		if err != nil && !readErr {
			firstErr, firstKey = err, name
			readErr = true
		}
		return v, err
	}
	// Get returns ("", nil) for missing keys; only override when a value
	// is actually stored.
	if enabled, err := read("musicbrainz.enabled"); err == nil && enabled != "" {
		mbCfg.Enabled = enabled == "true"
	}
	if url, err := read("musicbrainz.api_url"); err == nil && url != "" {
		mbCfg.APIURL = url
	}
	if rl, err := read("musicbrainz.rate_limit"); err == nil && rl != "" {
		if n, err := strconv.Atoi(rl); err != nil || n <= 0 {
			logger.Warn("[scanner] invalid musicbrainz rate limit %q", rl)
		} else {
			mbCfg.RateLimit = n
		}
	}

	neteaseEnabled := s.neteaseEnabled
	// Get returns ("", nil) for missing keys; only override when a value
	// is actually stored.
	if enabled, err := read("netease.enabled"); err == nil && enabled != "" {
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
	run := s.activeScan[libraryID]
	if run == nil {
		return nil
	}
	// Return a copy: the caller JSON-encodes it outside the lock while the
	// scan goroutine keeps mutating the shared progress struct.
	cp := *run.progress
	return &cp
}

// BeginLibraryDelete marks a library as being deleted so StartScan rejects it
// while teardown runs. EndLibraryDelete clears the mark (deferred by the
// caller so it also runs when the delete is aborted after a cancel timeout).
func (s *ScannerService) BeginLibraryDelete(libraryID string) {
	s.mu.Lock()
	s.deleting[libraryID] = true
	s.mu.Unlock()
}

func (s *ScannerService) EndLibraryDelete(libraryID string) {
	s.mu.Lock()
	delete(s.deleting, libraryID)
	s.mu.Unlock()
}

// CancelScan stops a running scan for a library (if any) and waits (bounded by
// scanCancelWait or ctx) for its goroutine to unwind, reporting whether it
// actually stopped. The lookup + cancel happen under the lock so they cannot
// race a normal completion (TOCTOU), and the caller is expected to hold a
// BeginLibraryDelete mark so no new scan can be installed during the wait.
func (s *ScannerService) CancelScan(ctx context.Context, libraryID string) bool {
	s.mu.Lock()
	run := s.activeScan[libraryID]
	if run != nil {
		run.cancel()
	}
	s.mu.Unlock()
	if run == nil {
		return true
	}
	select {
	case <-run.done:
		logger.Info("[scanner] cancelled scan for library=%s", libraryID)
		return true
	case <-ctx.Done():
		logger.Warn("[scanner] cancel for library=%s aborted: %v", libraryID, ctx.Err())
		return false
	case <-time.After(scanCancelWait):
		logger.Warn("[scanner] scan for library=%s did not stop within %s", libraryID, scanCancelWait)
		return false
	}
}

// clearActive removes a finished run's entry, but only when it is still the
// current one: after CancelScan an old goroutine may finish while a newer scan
// already owns the library, and it must not clear the newer run.
func (s *ScannerService) clearActive(libraryID string, run *scanRun) {
	s.mu.Lock()
	if s.activeScan[libraryID] == run {
		delete(s.activeScan, libraryID)
	}
	s.mu.Unlock()
}

func (s *ScannerService) StartScan(ctx context.Context, libraryID string, mode string) error {
	s.mu.Lock()
	if s.deleting[libraryID] {
		s.mu.Unlock()
		return fmt.Errorf("library %s is being deleted", libraryID)
	}
	if _, running := s.activeScan[libraryID]; running {
		s.mu.Unlock()
		return fmt.Errorf("scan already running for library %s", libraryID)
	}
	s.rebuildEngine(ctx)
	scanCtx, cancel := context.WithCancel(context.Background())
	run := &scanRun{
		progress: &ScanProgress{LibraryID: libraryID, Status: "running"},
		cancel:   cancel,
		done:     make(chan struct{}),
	}
	s.activeScan[libraryID] = run
	// Snapshot the engine while still holding the lock: rebuildEngine just
	// swapped it, and the scan goroutine must run against the engine it was
	// started with (concurrent scans of other libraries may rebuild it again
	// meanwhile).
	engine := s.engine
	s.mu.Unlock()

	if mode != "overwrite" {
		mode = "missing"
	}
	go s.runScan(scanCtx, libraryID, mode, engine, run)
	return nil
}

func (s *ScannerService) runScan(ctx context.Context, libraryID, mode string, engine *scanner.Engine, run *scanRun) {
	defer close(run.done)
	defer s.clearActive(libraryID, run)

	lib, err := s.libRepo.FindByID(ctx, libraryID)
	if err != nil {
		s.setError(libraryID, run, fmt.Sprintf("library not found: %v", err))
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
		if s.activeScan[libraryID] == run {
			run.progress.TotalFiles = stats.TotalFiles
			run.progress.Scanned = stats.Scanned
			run.progress.NewTracks = stats.NewTracks
			run.progress.UpdatedTracks = stats.UpdatedTracks
			run.progress.DeletedTracks = stats.DeletedTracks
			run.progress.Errors = len(stats.Errors)
		}
		s.mu.Unlock()
	})
	// ScanLibrary may return (nil, err) on DB failure — never dereference nil stats.
	if stats == nil {
		stats = &scanner.ScanStats{}
	}
	// A cancellation is identified by the error the engine returns (it returns
	// ctx.Err() once it observes the cancelled context), NOT by whether a cancel
	// was requested: a distinct non-context failure that races a cancel must
	// still be reported as failed, not masked as cancelled.
	cancelled := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)

	// Finalize on a fresh context: a cancelled scan's own context is already
	// done, so it could not persist the terminal state.
	bg, bgCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer bgCancel()

	s.mu.Lock()
	switch {
	case cancelled:
		run.progress.Status = "cancelled"
	case err != nil:
		run.progress.Status = "failed"
	default:
		run.progress.Status = "completed"
	}
	s.mu.Unlock()

	completedAt := time.Now()
	switch {
	case cancelled:
		job.Status = "cancelled"
	case err != nil:
		job.Status = "failed"
	default:
		job.Status = "completed"
	}
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
	s.scanRepo.Update(bg, job)

	// A cancelled scan (library deleted mid-scan) must not touch the library
	// row or notify: the library is gone and the scan never produced a result.
	if cancelled {
		logger.Info("[scanner] cancelled library=%s after new=%d updated=%d", libraryID, job.NewTracks, job.UpdatedTracks)
		return
	}

	lib.LastScanErrors = len(stats.Errors)
	lib.UpdatedAt = time.Now()
	s.libRepo.UpdateStats(bg, lib)

	logger.Info("[scanner] finished library=%s status=%s new=%d updated=%d deleted=%d errors=%d",
		libraryID, job.Status, job.NewTracks, job.UpdatedTracks, job.DeletedTracks, len(stats.Errors))

	if s.notif != nil {
		if err != nil {
			if nerr := s.notif.NotifyScanFailed(ctx, lib.Name, err.Error()); nerr != nil {
				logger.Error("[scanner] failed to send scan failure notification: %v", nerr)
			}
		} else {
			if nerr := s.notif.NotifyScanComplete(ctx, job, lib.Name); nerr != nil {
				logger.Error("[scanner] failed to send scan completion notification: %v", nerr)
			}
		}
	}
}

func (s *ScannerService) setError(libraryID string, run *scanRun, msg string) {
	if run != nil {
		s.mu.Lock()
		if s.activeScan[libraryID] == run {
			run.progress.Status = "failed"
		}
		s.mu.Unlock()
	}
	logger.Error("[scanner] error library=%s: %s", libraryID, msg)
}
