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

func testAlbum() *domain.Album {
	now := time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	return &domain.Album{
		ID:        "alb-001",
		Title:     "Abbey Road",
		ArtistID:  "a-001",
		Year:      1969,
		Genre:     "Rock",
		SongCount: 17,
		Duration:  2823,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func albumRows(a *domain.Album) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "title", "artist_id", "mbid", "country", "year", "genre", "cover_image_id", "song_count", "duration", "created_at", "updated_at"}).
		AddRow(a.ID, a.Title, a.ArtistID, a.MBID, a.Country, a.Year, a.Genre, nil, a.SongCount, a.Duration, a.CreatedAt, a.UpdatedAt)
}

func TestAlbumRepoFindByID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAlbumRepo(db)
	a := testAlbum()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE id = $1`)).
		WithArgs("alb-001").
		WillReturnRows(albumRows(a))

	got, err := repo.FindByID(context.Background(), "alb-001")
	require.NoError(t, err)
	assert.Equal(t, a.Title, got.Title)
	assert.Equal(t, 1969, got.Year)
	assert.Nil(t, got.CoverImageID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAlbumRepoFindByIDWithCover(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAlbumRepo(db)

	rows := sqlmock.NewRows([]string{"id", "title", "artist_id", "mbid", "country", "year", "genre", "cover_image_id", "song_count", "duration", "created_at", "updated_at"}).
		AddRow("alb-001", "X", "a-1", "", "", 2000, "", "img-9", 1, 100, time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE id = $1`)).
		WithArgs("alb-001").
		WillReturnRows(rows)

	got, err := repo.FindByID(context.Background(), "alb-001")
	require.NoError(t, err)
	require.NotNil(t, got.CoverImageID)
	assert.Equal(t, "img-9", *got.CoverImageID)
}

func TestAlbumRepoFindByLibraryID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAlbumRepo(db)
	a1, a2 := testAlbum(), testAlbum()
	a2.ID, a2.Title = "alb-002", "Let It Be"

	rows := sqlmock.NewRows([]string{"id", "title", "artist_id", "mbid", "country", "year", "genre", "cover_image_id", "song_count", "duration", "created_at", "updated_at"}).
		AddRow(a1.ID, a1.Title, a1.ArtistID, a1.MBID, a1.Country, a1.Year, a1.Genre, nil, a1.SongCount, a1.Duration, a1.CreatedAt, a1.UpdatedAt).
		AddRow(a2.ID, a2.Title, a2.ArtistID, a2.MBID, a2.Country, a2.Year, a2.Genre, nil, a2.SongCount, a2.Duration, a2.CreatedAt, a2.UpdatedAt)

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE t.library_id = $1`)).
		WithArgs("lib-1").
		WillReturnRows(rows)

	got, err := repo.FindByLibraryID(context.Background(), "lib-1")
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestAlbumRepoFindByArtistID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAlbumRepo(db)
	a := testAlbum()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE artist_id = $1 ORDER BY year DESC`)).
		WithArgs("a-001").
		WillReturnRows(albumRows(a))

	got, err := repo.FindByArtistID(context.Background(), "a-001")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, a.ID, got[0].ID)
}

func TestAlbumRepoBatchCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAlbumRepo(db)
	a1, a2 := testAlbum(), testAlbum()
	a2.ID = "alb-002"

	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO albums (id, title, artist_id, mbid, country, year, genre, cover_image_id, song_count, duration, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO albums`)).
		WithArgs(a1.ID, a1.Title, a1.ArtistID, a1.MBID, a1.Country, a1.Year, a1.Genre, a1.CoverImageID, a1.SongCount, a1.Duration, a1.CreatedAt, a1.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO albums`)).
		WithArgs(a2.ID, a2.Title, a2.ArtistID, a2.MBID, a2.Country, a2.Year, a2.Genre, a2.CoverImageID, a2.SongCount, a2.Duration, a2.CreatedAt, a2.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.BatchCreate(context.Background(), []domain.Album{*a1, *a2}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAlbumRepoUpdate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAlbumRepo(db)
	a := testAlbum()
	a.Title = "Renamed"

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE albums SET title=$1, artist_id=$2, mbid=$3, country=$4, year=$5, genre=$6,
		 cover_image_id=$7, song_count=$8, duration=$9, updated_at=NOW()
		 WHERE id=$10`)).
		WithArgs(a.Title, a.ArtistID, a.MBID, a.Country, a.Year, a.Genre, a.CoverImageID, a.SongCount, a.Duration, a.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Update(context.Background(), a))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAlbumRepoFindByNameAndArtist(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAlbumRepo(db)
	a := testAlbum()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE title = $1 AND artist_id = $2`)).
		WithArgs("Abbey Road", "a-001").
		WillReturnRows(albumRows(a))

	got, err := repo.FindByNameAndArtist(context.Background(), "Abbey Road", "a-001", "lib-1")
	require.NoError(t, err)
	assert.Equal(t, a.ID, got.ID)
}

func TestAlbumRepoFindByMBID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAlbumRepo(db)
	a := testAlbum()
	a.MBID = "f27ec8db-af05-4f36-916d-5e1c4c52b5e1"

	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE mbid = $1`)).
		WithArgs(a.MBID).
		WillReturnRows(albumRows(a))

	got, err := repo.FindByMBID(context.Background(), a.MBID)
	require.NoError(t, err)
	assert.Equal(t, a.ID, got.ID)
}
