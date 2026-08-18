package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/domain"
)

func TestScanJobRepoCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewScanJobRepo(db)
	job := &domain.ScanJob{
		ID: "scan-001", LibraryID: "lib-001", Type: "full", Status: "running",
		TotalFiles: 100, Scanned: 10, CreatedAt: time.Now(),
	}

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO scan_jobs (id, library_id, type, status, total_files, scanned,
		 new_tracks, updated_tracks, deleted_tracks, errors, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`)).
		WithArgs(job.ID, job.LibraryID, job.Type, job.Status, job.TotalFiles, job.Scanned,
			job.NewTracks, job.UpdatedTracks, job.DeletedTracks, job.Errors, job.CreatedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, repo.Create(context.Background(), job))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScanJobRepoFindLatestByLibraryID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewScanJobRepo(db)
	completedAt := time.Date(2024, 7, 1, 10, 0, 0, 0, time.UTC)

	rows := sqlmock.NewRows([]string{"id", "library_id", "type", "status", "total_files", "scanned",
		"new_tracks", "updated_tracks", "deleted_tracks", "errors", "created_at", "completed_at"}).
		AddRow("scan-001", "lib-001", "full", "completed", 100, 100, 5, 2, 0, "", time.Now(), completedAt)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM scan_jobs WHERE library_id = $1 ORDER BY created_at DESC LIMIT 1`)).
		WithArgs("lib-001").
		WillReturnRows(rows)

	job, err := repo.FindLatestByLibraryID(context.Background(), "lib-001")
	require.NoError(t, err)
	assert.Equal(t, "scan-001", job.ID)
	assert.Equal(t, "completed", job.Status)
	require.NotNil(t, job.CompletedAt)
	assert.Equal(t, completedAt, *job.CompletedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScanJobRepoFindLatestNoRows(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewScanJobRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM scan_jobs WHERE library_id = $1 ORDER BY created_at DESC LIMIT 1`)).
		WithArgs("lib-001").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindLatestByLibraryID(context.Background(), "lib-001")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestScanJobRepoUpdate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewScanJobRepo(db)
	job := &domain.ScanJob{ID: "scan-001", Status: "completed", TotalFiles: 10, Scanned: 10}

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE scan_jobs SET status=$1, total_files=$2, scanned=$3,
		 new_tracks=$4, updated_tracks=$5, deleted_tracks=$6, errors=$7,
		 completed_at=$8 WHERE id=$9`)).
		WithArgs(job.Status, job.TotalFiles, job.Scanned, job.NewTracks, job.UpdatedTracks,
			job.DeletedTracks, job.Errors, job.CompletedAt, job.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Update(context.Background(), job))
	require.NoError(t, mock.ExpectationsWereMet())
}
