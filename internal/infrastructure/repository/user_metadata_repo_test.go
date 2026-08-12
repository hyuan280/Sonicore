package repository

import (
	"context"
	"database/sql"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testUserMetadata() *UserMetadata {
	return &UserMetadata{
		UserID:      "u-001",
		FileHash:    "abc123",
		TrackMBID:   "mbid-1",
		Title:       "Song",
		Artist:      "Artist",
		Album:       "Album",
		AlbumArtist: "Artist",
		TrackNumber: 1,
		DiscNumber:  1,
		Year:        2024,
		Genre:       "Rock",
	}
}

func TestUserMetadataRepoFindByUserAndHash(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserMetadataRepo(db)
	m := testUserMetadata()

	rows := sqlmock.NewRows([]string{"user_id", "file_hash", "track_mbid", "title", "artist", "album", "album_artist",
		"track_number", "disc_number", "year", "genre"}).
		AddRow(m.UserID, m.FileHash, m.TrackMBID, m.Title, m.Artist, m.Album, m.AlbumArtist,
			m.TrackNumber, m.DiscNumber, m.Year, m.Genre)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM user_metadata WHERE user_id = $1 AND file_hash = $2`)).
		WithArgs("u-001", "abc123").
		WillReturnRows(rows)

	got, err := repo.FindByUserAndHash(context.Background(), "u-001", "abc123")
	require.NoError(t, err)
	assert.Equal(t, m, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserMetadataRepoFindMissing(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserMetadataRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM user_metadata WHERE user_id = $1 AND file_hash = $2`)).
		WithArgs("u-001", "missing").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindByUserAndHash(context.Background(), "u-001", "missing")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestUserMetadataRepoUpsert(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserMetadataRepo(db)
	m := testUserMetadata()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO user_metadata (user_id, file_hash, track_mbid, title, artist, album, album_artist, track_number, disc_number, year, genre, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
		 ON CONFLICT (user_id, file_hash) DO UPDATE SET
		 track_mbid=$3, title=$4, artist=$5, album=$6, album_artist=$7,
		 track_number=$8, disc_number=$9, year=$10, genre=$11, updated_at=NOW()`)).
		WithArgs(m.UserID, m.FileHash, m.TrackMBID, m.Title, m.Artist, m.Album, m.AlbumArtist,
			m.TrackNumber, m.DiscNumber, m.Year, m.Genre).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Upsert(context.Background(), m))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserMetadataRepoDeleteByUserAndHash(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewUserMetadataRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM user_metadata WHERE user_id = $1 AND file_hash = $2`)).
		WithArgs("u-001", "abc123").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.DeleteByUserAndHash(context.Background(), "u-001", "abc123"))
	require.NoError(t, mock.ExpectationsWereMet())
}
