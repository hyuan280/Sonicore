package download

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type Manager struct {
	db      *sql.DB
	jobRepo *repository.DownloadJobRepo
	sources []port.DownloadSource
	mu      sync.Mutex
	active  map[string]context.CancelFunc
}

func NewManager(db *sql.DB) *Manager {
	m := &Manager{
		db:      db,
		jobRepo: repository.NewDownloadJobRepo(db),
		active:  make(map[string]context.CancelFunc),
	}

	m.Register(NewDirectSource())
	return m
}

func (m *Manager) Register(src port.DownloadSource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sources = append(m.sources, src)
	logger.Info("[download] registered source: %s", src.Name())
}

func (m *Manager) MatchSource(url string) port.DownloadSource {
	for _, src := range m.sources {
		if src.Match(url) {
			return src
		}
	}
	return nil
}

func (m *Manager) CreateJob(ctx context.Context, url string, libraryID string) (*domain.DownloadJob, error) {
	source := m.MatchSource(url)
	if source == nil {
		return nil, &ErrNoSource{URL: url}
	}

	now := time.Now()
	job := &domain.DownloadJob{
		ID:        domain.NewID(),
		URL:       url,
		Source:    source.Name(),
		LibraryID: libraryID,
		Status:    "pending",
		Progress:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := m.jobRepo.Create(ctx, job); err != nil {
		return nil, err
	}

	go m.runJob(job.ID, source)
	return job, nil
}

func (m *Manager) runJob(jobID string, source port.DownloadSource) {
	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.active[jobID] = cancel
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.active, jobID)
		m.mu.Unlock()
	}()

	job, err := m.jobRepo.FindByID(ctx, jobID)
	if err != nil {
		logger.Warn("[download] job %s not found: %v", jobID, err)
		return
	}

	job.Status = "downloading"
	m.jobRepo.Update(ctx, job)

	if err := source.Fetch(ctx, job); err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		m.jobRepo.Update(ctx, job)
		logger.Error("[download] job %s failed: %v", jobID, err)
		return
	}

	job.Status = "processing"
	m.jobRepo.Update(ctx, job)

	if err := m.postProcess(ctx, job); err != nil {
		logger.Info("[download] post-process %s: %v", jobID, err)
	}

	job.Status = "completed"
	job.Progress = 100
	m.jobRepo.Update(ctx, job)
	logger.Info("[download] job %s completed → %s", jobID, job.TargetPath)
}

func (m *Manager) postProcess(ctx context.Context, job *domain.DownloadJob) error {
	if !metadata.IsAudioFile(job.TargetPath) {
		return nil
	}

	meta, err := metadata.Probe(job.TargetPath)
	if err != nil {
		return err
	}

	_ = meta
	return nil
}

func (m *Manager) Cancel(jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cancel, ok := m.active[jobID]; ok {
		cancel()
	}
}

func (m *Manager) List(ctx context.Context, libraryID string) ([]domain.DownloadJob, error) {
	return m.jobRepo.FindByLibraryID(ctx, libraryID)
}

func (m *Manager) Get(ctx context.Context, jobID string) (*domain.DownloadJob, error) {
	return m.jobRepo.FindByID(ctx, jobID)
}

type ErrNoSource struct {
	URL string
}

func (e *ErrNoSource) Error() string {
	return "no download source supports URL: " + e.URL
}
