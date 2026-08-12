package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLibrary() *domain.Library {
	now := time.Date(2024, 5, 1, 8, 0, 0, 0, time.UTC)
	return &domain.Library{
		ID:                  "lib-001",
		Name:                "Main",
		Path:                "/music",
		OwnerID:             "u-001",
		MetadataStorageMode: "database",
		ScanInterval:        "1h",
		TrackCount:          100,
		Duration:            3600,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func libraryRows(l *domain.Library, lastScannedAt *time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
		"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
		AddRow(l.ID, l.Name, l.Path, l.OwnerID, l.MetadataStorageMode, l.ScanInterval,
			lastScannedAt, l.LastScanErrors, l.TrackCount, l.Duration, l.CreatedAt, l.UpdatedAt)
}

func TestLibraryRepoCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)
	l := testLibrary()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO libraries (id, name, path, owner_id, metadata_storage_mode, scan_interval,
		track_count, duration, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`)).
		WithArgs(l.ID, l.Name, l.Path, l.OwnerID, l.MetadataStorageMode, l.ScanInterval, l.TrackCount, l.Duration, l.CreatedAt, l.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, repo.Create(context.Background(), l))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryRepoFindByID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)
	l := testLibrary()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM libraries WHERE id = $1`)).
		WithArgs("lib-001").
		WillReturnRows(libraryRows(l, nil))

	got, err := repo.FindByID(context.Background(), "lib-001")
	require.NoError(t, err)
	assert.Equal(t, l, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryRepoFindByIDWithLastScanned(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)
	l := testLibrary()
	scanned := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM libraries WHERE id = $1`)).
		WithArgs("lib-001").
		WillReturnRows(libraryRows(l, &scanned))

	got, err := repo.FindByID(context.Background(), "lib-001")
	require.NoError(t, err)
	require.NotNil(t, got.LastScannedAt)
	assert.Equal(t, scanned, *got.LastScannedAt)
}

func TestLibraryRepoFindByOwnerID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)
	l := testLibrary()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM libraries WHERE owner_id = $1 ORDER BY name`)).
		WithArgs("u-001").
		WillReturnRows(libraryRows(l, nil))

	got, err := repo.FindByOwnerID(context.Background(), "u-001")
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestLibraryRepoFindByUserID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)
	l := testLibrary()

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE lm.user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(libraryRows(l, nil))

	got, err := repo.FindByUserID(context.Background(), "u-001")
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestLibraryRepoAddMember(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)
	now := time.Now()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO library_members (library_id, user_id, role, joined_at)
		 VALUES ($1, $2, $3, $4)`)).
		WithArgs("lib-001", "u-002", "viewer", now).
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, repo.AddMember(context.Background(), &domain.LibraryMember{
		LibraryID: "lib-001", UserID: "u-002", Role: "viewer", JoinedAt: now,
	}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryRepoRemoveMember(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM library_members WHERE library_id = $1 AND user_id = $2`)).
		WithArgs("lib-001", "u-002").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.RemoveMember(context.Background(), "lib-001", "u-002"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryRepoUpdateMemberRole(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE library_members SET role = $1 WHERE library_id = $2 AND user_id = $3`)).
		WithArgs("contributor", "lib-001", "u-002").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.UpdateMemberRole(context.Background(), "lib-001", "u-002", "contributor"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryRepoGetMembers(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)
	now := time.Now()

	rows := sqlmock.NewRows([]string{"library_id", "user_id", "role", "joined_at"}).
		AddRow("lib-001", "u-001", "owner", now).
		AddRow("lib-001", "u-002", "viewer", now)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT library_id, user_id, role, joined_at FROM library_members WHERE library_id = $1`)).
		WithArgs("lib-001").
		WillReturnRows(rows)

	members, err := repo.GetMembers(context.Background(), "lib-001")
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.Equal(t, "u-001", members[0].UserID)
	assert.Equal(t, "viewer", members[1].Role)
}

func TestLibraryRepoGetMembersError(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT library_id, user_id, role, joined_at FROM library_members WHERE library_id = $1`)).
		WillReturnError(sql.ErrConnDone)

	_, err := repo.GetMembers(context.Background(), "lib-001")
	require.Error(t, err)
}

func TestLibraryRepoUpdateStats(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)
	l := testLibrary()
	scanned := time.Now()

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE libraries SET track_count=$2, duration=$3, last_scanned_at=$4, last_scan_errors=$5, updated_at=$6 WHERE id=$1`)).
		WithArgs(l.ID, l.TrackCount, l.Duration, scanned, l.LastScanErrors, l.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))

	l.LastScannedAt = &scanned
	require.NoError(t, repo.UpdateStats(context.Background(), l))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLibraryRepoDelete(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewLibraryRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM libraries WHERE id = $1`)).
		WithArgs("lib-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Delete(context.Background(), "lib-001"))
	require.NoError(t, mock.ExpectationsWereMet())
}
