package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/domain"
)

func testDownloadJob() *domain.DownloadJob {
	now := time.Date(2024, 9, 1, 18, 0, 0, 0, time.UTC)
	return &domain.DownloadJob{
		ID:         "dl-001",
		URL:        "https://example.com/song.mp3",
		Source:     "netease",
		LibraryID:  "lib-001",
		Format:     "mp3",
		Status:     "running",
		Progress:   0.5,
		TargetPath: "/music/song.mp3",
		Metadata:   `{"title":"Song"}`,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func downloadRows(j *domain.DownloadJob) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "url", "source", "library_id", "format", "status",
		"progress", "target_path", "metadata", "error", "created_at", "updated_at"}).
		AddRow(j.ID, j.URL, j.Source, j.LibraryID, j.Format, j.Status,
			j.Progress, j.TargetPath, j.Metadata, j.Error, j.CreatedAt, j.UpdatedAt)
}

func TestDownloadJobRepoCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewDownloadJobRepo(db)
	j := testDownloadJob()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO download_jobs (id, url, source, library_id, format, status,
		 progress, target_path, metadata, error, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`)).
		WithArgs(j.ID, j.URL, j.Source, j.LibraryID, j.Format, j.Status,
			j.Progress, j.TargetPath, j.Metadata, j.Error, j.CreatedAt, j.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, repo.Create(context.Background(), j))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDownloadJobRepoFindByID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewDownloadJobRepo(db)
	j := testDownloadJob()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM download_jobs WHERE id = $1`)).
		WithArgs("dl-001").
		WillReturnRows(downloadRows(j))

	got, err := repo.FindByID(context.Background(), "dl-001")
	require.NoError(t, err)
	assert.Equal(t, j, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDownloadJobRepoFindByLibraryID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewDownloadJobRepo(db)
	j1, j2 := testDownloadJob(), testDownloadJob()
	j2.ID = "dl-002"

	rows := sqlmock.NewRows([]string{"id", "url", "source", "library_id", "format", "status",
		"progress", "target_path", "metadata", "error", "created_at", "updated_at"}).
		AddRow(j1.ID, j1.URL, j1.Source, j1.LibraryID, j1.Format, j1.Status, j1.Progress, j1.TargetPath, j1.Metadata, j1.Error, j1.CreatedAt, j1.UpdatedAt).
		AddRow(j2.ID, j2.URL, j2.Source, j2.LibraryID, j2.Format, j2.Status, j2.Progress, j2.TargetPath, j2.Metadata, j2.Error, j2.CreatedAt, j2.UpdatedAt)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM download_jobs WHERE library_id = $1 ORDER BY created_at DESC`)).
		WithArgs("lib-001").
		WillReturnRows(rows)

	got, err := repo.FindByLibraryID(context.Background(), "lib-001")
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestDownloadJobRepoUpdate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewDownloadJobRepo(db)
	j := testDownloadJob()
	j.Status = "completed"
	j.Progress = 1

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE download_jobs SET source=$1, format=$2, status=$3, progress=$4,
		 target_path=$5, metadata=$6, error=$7, updated_at=NOW()
		 WHERE id=$8`)).
		WithArgs(j.Source, j.Format, j.Status, j.Progress, j.TargetPath, j.Metadata, j.Error, j.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Update(context.Background(), j))
	require.NoError(t, mock.ExpectationsWereMet())
}
