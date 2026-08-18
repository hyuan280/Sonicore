package repository

import (
	"context"
	"database/sql"

	"github.com/sonicore/server/internal/core/domain"
)

type ScanJobRepo struct {
	db *sql.DB
}

func NewScanJobRepo(db *sql.DB) *ScanJobRepo {
	return &ScanJobRepo{db: db}
}

func (r *ScanJobRepo) Create(ctx context.Context, job *domain.ScanJob) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO scan_jobs (id, library_id, type, status, total_files, scanned,
		 new_tracks, updated_tracks, deleted_tracks, errors, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		job.ID, job.LibraryID, job.Type, job.Status, job.TotalFiles, job.Scanned,
		job.NewTracks, job.UpdatedTracks, job.DeletedTracks, job.Errors, job.CreatedAt)
	return err
}

func (r *ScanJobRepo) FindLatestByLibraryID(ctx context.Context, libraryID string) (*domain.ScanJob, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, type, status, total_files, scanned,
		 new_tracks, updated_tracks, deleted_tracks, errors, created_at, completed_at
		 FROM scan_jobs WHERE library_id = $1 ORDER BY created_at DESC LIMIT 1`,
		libraryID)
	return scanScanJob(row)
}

func (r *ScanJobRepo) Update(ctx context.Context, job *domain.ScanJob) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE scan_jobs SET status=$1, total_files=$2, scanned=$3,
		 new_tracks=$4, updated_tracks=$5, deleted_tracks=$6, errors=$7,
		 completed_at=$8 WHERE id=$9`,
		job.Status, job.TotalFiles, job.Scanned,
		job.NewTracks, job.UpdatedTracks, job.DeletedTracks,
		job.Errors, job.CompletedAt, job.ID)
	return err
}

func scanScanJob(scanner interface {
	Scan(dest ...interface{}) error
}) (*domain.ScanJob, error) {
	var j domain.ScanJob
	err := scanner.Scan(&j.ID, &j.LibraryID, &j.Type, &j.Status,
		&j.TotalFiles, &j.Scanned, &j.NewTracks, &j.UpdatedTracks,
		&j.DeletedTracks, &j.Errors, &j.CreatedAt, &j.CompletedAt)
	if err != nil {
		return nil, err
	}
	return &j, nil
}
