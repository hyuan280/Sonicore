package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testArtist() *domain.Artist {
	now := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)
	return &domain.Artist{
		ID:         "a-001",
		Name:       "The Beatles",
		SortName:   "Beatles, The",
		MBID:       "b10bbbfc-cf9e-42e0-be17-ab2d0b1e5d0f",
		Country:    "GB",
		Biography:  "Rock band",
		TrackCount: 0,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func artistRows(a *domain.Artist, coverID *string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "sort_name", "mbid", "country", "biography", "cover_image_id", "track_count", "created_at", "updated_at", "roles"}).
		AddRow(a.ID, a.Name, a.SortName, a.MBID, a.Country, a.Biography, coverID, a.TrackCount, a.CreatedAt, a.UpdatedAt, "")
}

func TestArtistRepoFindByID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewArtistRepo(db)
	a := testArtist()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, sort_name, mbid, country, biography, cover_image_id, 0 AS track_count, created_at, updated_at,
		 COALESCE((SELECT string_agg(DISTINCT ta.role, ',' ORDER BY ta.role) FROM track_artists ta WHERE ta.artist_id = $1), '') AS roles
		 FROM artists WHERE id = $1`)).
		WithArgs("a-001").
		WillReturnRows(artistRows(a, nil))

	got, err := repo.FindByID(context.Background(), "a-001")
	require.NoError(t, err)
	assert.Equal(t, a.ID, got.ID)
	assert.Equal(t, a.Name, got.Name)
	assert.Nil(t, got.CoverImageID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestArtistRepoFindByIDWithCoverAndRoles(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewArtistRepo(db)

	rows := sqlmock.NewRows([]string{"id", "name", "sort_name", "mbid", "country", "biography", "cover_image_id", "track_count", "created_at", "updated_at", "roles"}).
		AddRow("a-001", "X", "", "", "", "", "img-1", 0, time.Now(), time.Now(), "lead_vocal,backing_vocal")

	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE id = $1`)).
		WithArgs("a-001").
		WillReturnRows(rows)

	got, err := repo.FindByID(context.Background(), "a-001")
	require.NoError(t, err)
	require.NotNil(t, got.CoverImageID)
	assert.Equal(t, "img-1", *got.CoverImageID)
	assert.Equal(t, []string{"lead_vocal", "backing_vocal"}, got.Roles)
}

func TestArtistRepoFindByLibraryID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewArtistRepo(db)
	a1, a2 := testArtist(), testArtist()
	a2.ID, a2.Name = "a-002", "Queen"

	rows := sqlmock.NewRows([]string{"id", "name", "sort_name", "mbid", "country", "biography", "cover_image_id", "track_count", "created_at", "updated_at", "roles"}).
		AddRow(a1.ID, a1.Name, "", "", "", "", nil, 0, a1.CreatedAt, a1.UpdatedAt, "").
		AddRow(a2.ID, a2.Name, "", "", "", "", nil, 0, a2.CreatedAt, a2.UpdatedAt, "")

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE t.library_id = $1`)).
		WithArgs("lib-1").
		WillReturnRows(rows)

	got, err := repo.FindByLibraryID(context.Background(), "lib-1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, a1.ID, got[0].ID)
	assert.Equal(t, a2.ID, got[1].ID)
}

func TestArtistRepoBatchCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewArtistRepo(db)
	a1, a2 := testArtist(), testArtist()
	a2.ID = "a-002"

	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO artists (id, name, sort_name, mbid, country, biography, cover_image_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO artists`)).
		WithArgs(a1.ID, a1.Name, a1.SortName, a1.MBID, a1.Country, a1.Biography, a1.CoverImageID, a1.CreatedAt, a1.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO artists`)).
		WithArgs(a2.ID, a2.Name, a2.SortName, a2.MBID, a2.Country, a2.Biography, a2.CoverImageID, a2.CreatedAt, a2.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.BatchCreate(context.Background(), []domain.Artist{*a1, *a2}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestArtistRepoBatchCreateRollbackOnError(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewArtistRepo(db)
	a := testArtist()

	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO artists`))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO artists`)).
		WillReturnError(sqlmock.ErrCancelled)
	mock.ExpectRollback()

	err := repo.BatchCreate(context.Background(), []domain.Artist{*a})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestArtistRepoUpdate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewArtistRepo(db)
	a := testArtist()
	a.Name = "Renamed"

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE artists SET name=$1, sort_name=$2, mbid=$3, country=$4, biography=$5,
		 cover_image_id=$6, updated_at=NOW()
		 WHERE id=$7`)).
		WithArgs(a.Name, a.SortName, a.MBID, a.Country, a.Biography, a.CoverImageID, a.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Update(context.Background(), a))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestArtistRepoFindByMBID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewArtistRepo(db)
	a := testArtist()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE mbid = $1`)).
		WithArgs(a.MBID).
		WillReturnRows(artistRows(a, nil))

	got, err := repo.FindByMBID(context.Background(), a.MBID)
	require.NoError(t, err)
	assert.Equal(t, a.ID, got.ID)
}

func TestArtistRepoFindByName(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewArtistRepo(db)
	a := testArtist()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE name = $1`)).
		WithArgs("The Beatles").
		WillReturnRows(artistRows(a, nil))

	got, err := repo.FindByName(context.Background(), "The Beatles")
	require.NoError(t, err)
	assert.Equal(t, a.ID, got.ID)
}

func TestArtistRepoScanError(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewArtistRepo(db)

	rows := sqlmock.NewRows([]string{"id", "name"}).AddRow("a-001", "X")
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE id = $1`)).
		WithArgs("a-001").
		WillReturnRows(rows)

	_, err := repo.FindByID(context.Background(), "a-001")
	require.Error(t, err)
}
