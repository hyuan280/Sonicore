package repository

import (
	"context"
	"database/sql"

	"github.com/sonicore/server/internal/core/domain"
)

type DownloadJobRepo struct {
	db *sql.DB
}

func NewDownloadJobRepo(db *sql.DB) *DownloadJobRepo {
	return &DownloadJobRepo{db: db}
}

func (r *DownloadJobRepo) Create(ctx context.Context, job *domain.DownloadJob) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO download_jobs (id, url, source, library_id, format, status,
		 progress, target_path, metadata, error, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		job.ID, job.URL, job.Source, job.LibraryID, job.Format, job.Status,
		job.Progress, job.TargetPath, job.Metadata, job.Error, job.CreatedAt, job.UpdatedAt)
	return err
}

func (r *DownloadJobRepo) FindByID(ctx context.Context, id string) (*domain.DownloadJob, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, url, source, library_id, format, status,
		 progress, target_path, metadata, error, created_at, updated_at
		 FROM download_jobs WHERE id = $1`, id)
	return scanDownloadJob(row)
}

func (r *DownloadJobRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.DownloadJob, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, url, source, library_id, format, status,
		 progress, target_path, metadata, error, created_at, updated_at
		 FROM download_jobs WHERE library_id = $1 ORDER BY created_at DESC`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []domain.DownloadJob
	for rows.Next() {
		j, err := scanDownloadJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *j)
	}
	return jobs, rows.Err()
}

func (r *DownloadJobRepo) Update(ctx context.Context, job *domain.DownloadJob) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE download_jobs SET source=$1, format=$2, status=$3, progress=$4,
		 target_path=$5, metadata=$6, error=$7, updated_at=NOW()
		 WHERE id=$8`,
		job.Source, job.Format, job.Status, job.Progress,
		job.TargetPath, job.Metadata, job.Error, job.ID)
	return err
}

func scanDownloadJob(scanner interface {
	Scan(dest ...interface{}) error
}) (*domain.DownloadJob, error) {
	var j domain.DownloadJob
	err := scanner.Scan(&j.ID, &j.URL, &j.Source, &j.LibraryID, &j.Format,
		&j.Status, &j.Progress, &j.TargetPath, &j.Metadata, &j.Error,
		&j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &j, nil
}
