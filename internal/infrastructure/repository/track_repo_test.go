package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTrack() *domain.Track {
	now := time.Date(2024, 10, 1, 20, 0, 0, 0, time.UTC)
	return &domain.Track{
		ID:          "t-001",
		LibraryID:   "lib-001",
		Title:       "Song",
		Duration:    240,
		BitRate:     320,
		SampleRate:  44100,
		Channels:    2,
		FilePath:    "/music/song.mp3",
		FileSize:    10_000_000,
		FileFormat:  "mp3",
		AudioCodec:  "mp3",
		MBID:         "mbid-1",
		MetadataSource: "musicbrainz",
		Hash:        "hash-1",
		LyricsMask:  0,
		LyricsOffset: 0,
		Heat:        1,
		PlayCount:   0,
		Version:     1,
		VersionLabel: "",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func trackColumns() []string {
	return []string{"id", "library_id", "title", "cover_image_id",
		"duration", "bit_rate", "sample_rate", "channels",
		"file_path", "file_size", "file_format", "audio_codec", "mbid", "metadata_source", "acoust_id", "hash",
		"lyrics_mask", "lyrics_offset", "heat", "play_count", "last_played_at", "metadata", "version", "version_label", "created_at", "updated_at"}
}

func trackValues(t *domain.Track) []driver.Value {
	vals := []driver.Value{
		t.ID, t.LibraryID, t.Title, t.CoverImageID,
		t.Duration, t.BitRate, t.SampleRate, t.Channels,
		t.FilePath, t.FileSize, t.FileFormat, t.AudioCodec, t.MBID, "musicbrainz", t.AcoustID, t.Hash,
		t.LyricsMask, t.LyricsOffset, t.Heat, t.PlayCount, t.LastPlayedAt, t.Metadata, t.Version, t.VersionLabel, t.CreatedAt, t.UpdatedAt,
	}
	return vals
}

func trackRows(t *domain.Track) *sqlmock.Rows {
	return sqlmock.NewRows(trackColumns()).AddRow(trackValues(t)...)
}

func TestTrackRepoFindByHash(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	track := testTrack()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE hash = $1`)).
		WithArgs("hash-1").
		WillReturnRows(trackRows(track))

	got, err := repo.FindByHash(context.Background(), "hash-1")
	require.NoError(t, err)
	assert.Equal(t, track.ID, got.ID)
	assert.Equal(t, track.Title, got.Title)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTrackRepoFindByLibraryID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	t1, t2 := testTrack(), testTrack()
	t2.ID = "t-002"

	rows := sqlmock.NewRows(trackColumns()).
		AddRow(trackValues(t1)...).
		AddRow(trackValues(t2)...)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE library_id = ANY($1)`)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	got, err := repo.FindByLibraryID(context.Background(), "lib-001")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, t1.ID, got[0].ID)
}

func TestTrackRepoFindByLibraryIDEmpty(t *testing.T) {
	db, _ := newMockDB(t)
	repo := NewTrackRepo(db)

	got, err := repo.FindByLibraryID(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTrackRepoFindByIDWithRelations(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	track := testTrack()

	// main query
	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnRows(trackRows(track))

	// LoadTrackAlbums
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_albums ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "album_id", "track_number", "disc_number", "title", "cover_image_id"}).
			AddRow("t-001", "alb-1", 1, 1, "Album", nil))

	// LoadTrackArtists
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "mbid"}).
			AddRow("t-001", "a-1", "main", 0, "Artist", ""))

	got, err := repo.FindByID(context.Background(), "t-001")
	require.NoError(t, err)
	assert.Equal(t, track.ID, got.ID)
	require.Len(t, got.Albums, 1)
	assert.Equal(t, "Album", got.Albums[0].Album.Title)
	require.Len(t, got.Artists, 1)
	assert.Equal(t, "Artist", got.Artists[0].Artist.Name)
	assert.Equal(t, got.Artists[0].Artist, got.Artist)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTrackRepoFindByIDNotfound(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.FindByID(context.Background(), "missing")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestTrackRepoFindByIDsEmpty(t *testing.T) {
	db, _ := newMockDB(t)
	repo := NewTrackRepo(db)

	got, err := repo.FindByIDs(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTrackRepoFindByIDs(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	t1, t2 := testTrack(), testTrack()
	t2.ID = "t-002"

	rows := sqlmock.NewRows(trackColumns()).
		AddRow(trackValues(t1)...).
		AddRow(trackValues(t2)...)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = ANY($1)`)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	// Bulk albums: only t-001 has one
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_albums ta`)).
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "album_id", "track_number", "disc_number", "title", "cover_image_id"}).
			AddRow("t-001", "alb-1", 1, 1, "Album", nil))

	// Bulk artists: t-002 has one
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "mbid"}).
			AddRow("t-002", "a-2", "main", 0, "Artist2", ""))

	got, err := repo.FindByIDs(context.Background(), []string{"t-001", "t-002"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Len(t, got[0].Albums, 1)
	require.Len(t, got[1].Artists, 1)
	assert.Equal(t, "Artist2", got[1].Artist.Name)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTrackRepoFindByArtistID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	track := testTrack()

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ta.artist_id = $1`)).
		WithArgs("a-1").
		WillReturnRows(sqlmock.NewRows(trackColumns()).AddRow(trackValues(track)...))

	// bulk loads after rows
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "mbid"}))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_albums ta`)).
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "album_id", "track_number", "disc_number", "title", "cover_image_id"}))

	got, err := repo.FindByArtistID(context.Background(), "a-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestTrackRepoFindByAlbumID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	track := testTrack()

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE ta.album_id = $1`)).
		WithArgs("alb-1").
		WillReturnRows(sqlmock.NewRows(trackColumns()).AddRow(trackValues(track)...))

	got, err := repo.FindByAlbumID(context.Background(), "alb-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, track.ID, got[0].ID)
}

func TestTrackRepoUpdateLyricsOffset(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE tracks SET lyrics_offset=$1, updated_at=NOW() WHERE id=$2`)).
		WithArgs(1.5, "t-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.UpdateLyricsOffset(context.Background(), "t-001", 1.5))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTrackRepoDeleteByFilePath(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`DELETE FROM tracks WHERE file_path = $1 AND library_id = $2 RETURNING id`)).
		WithArgs("/music/song.mp3", "lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("t-001"))

	id, err := repo.DeleteByFilePath(context.Background(), "/music/song.mp3", "lib-001")
	require.NoError(t, err)
	assert.Equal(t, "t-001", id)
}

func TestTrackRepoDeleteByFilePathNoRows(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`DELETE FROM tracks WHERE file_path = $1 AND library_id = $2 RETURNING id`)).
		WithArgs("/gone.mp3", "lib-001").
		WillReturnError(sql.ErrNoRows)

	id, err := repo.DeleteByFilePath(context.Background(), "/gone.mp3", "lib-001")
	require.NoError(t, err)
	assert.Equal(t, "", id)
}

func TestTrackRepoReplaceTrackArtists(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	artists := []*domain.TrackArtist{
		{TrackID: "t-001", ArtistID: "a-1", Role: "main"},
		{TrackID: "t-001", ArtistID: "a-2", Role: "feat", SortOrder: 5},
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM track_artists WHERE track_id = $1`)).
		WithArgs("t-001").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO track_artists (track_id, artist_id, role, sort_order)`))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO track_artists`)).
		WithArgs("t-001", "a-1", "main", 0). // SortOrder 0 falls back to index 0
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO track_artists`)).
		WithArgs("t-001", "a-2", "feat", 5).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.ReplaceTrackArtists(context.Background(), "t-001", artists))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTrackRepoReplaceTrackArtistsRollback(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM track_artists WHERE track_id = $1`)).
		WillReturnError(sqlmock.ErrCancelled)
	mock.ExpectRollback()

	err := repo.ReplaceTrackArtists(context.Background(), "t-001", nil)
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTrackRepoReplaceTrackAlbums(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	albums := []*domain.TrackAlbum{
		{TrackID: "t-001", AlbumID: "alb-1", TrackNumber: 1, DiscNumber: 1},
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM track_albums WHERE track_id = $1`)).
		WithArgs("t-001").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO track_albums (track_id, album_id, track_number, disc_number)`))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO track_albums`)).
		WithArgs("t-001", "alb-1", 1, 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.ReplaceTrackAlbums(context.Background(), "t-001", albums))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTrackRepoFindVersionsByMbid(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	track := testTrack()

	mock.ExpectQuery(regexp.QuoteMeta(`INNER JOIN track_version_groups g ON g.track_id = t.id`)).
		WithArgs("mbid-1", "t-001", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(trackColumns()).AddRow(trackValues(track)...))

	got, err := repo.FindVersionsByMbid(context.Background(), "mbid-1", "t-001", []string{"lib-001"})
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestTrackRepoFindVersionsByMbidBulkEmpty(t *testing.T) {
	db, _ := newMockDB(t)
	repo := NewTrackRepo(db)

	got, err := repo.FindVersionsByMbidBulk(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTrackRepoBatchCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	track := testTrack()
	track.Artists = []*domain.TrackArtist{{TrackID: "t-001", ArtistID: "a-1", Role: "main"}}
	track.Albums = []*domain.TrackAlbum{{TrackID: "t-001", AlbumID: "alb-1", TrackNumber: 1, DiscNumber: 1}}

	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO tracks`))
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO track_artists (track_id, artist_id, role, sort_order)`))
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO track_albums (track_id, album_id, track_number, disc_number)`))

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO tracks`)).
		WithArgs(
			track.ID, track.LibraryID, track.Title, track.CoverImageID,
			track.Duration, track.BitRate, track.SampleRate, track.Channels,
			track.FilePath, track.FileSize, track.FileFormat, track.AudioCodec, track.MBID, "musicbrainz", track.AcoustID, track.Hash,
			track.LyricsMask, track.LyricsOffset, track.Heat, track.PlayCount, track.Metadata, track.Version, track.VersionLabel, track.CreatedAt, track.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO track_artists`)).
		WithArgs("t-001", "a-1", "main", 0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO track_albums`)).
		WithArgs("t-001", "alb-1", 1, 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.BatchCreate(context.Background(), []domain.Track{*track}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTrackRepoBatchCreateRollback(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewTrackRepo(db)
	track := testTrack()

	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO tracks`))
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO track_artists (track_id, artist_id, role, sort_order)`))
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO track_albums (track_id, album_id, track_number, disc_number)`))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO tracks`)).
		WillReturnError(sqlmock.ErrCancelled)
	mock.ExpectRollback()

	err := repo.BatchCreate(context.Background(), []domain.Track{*track})
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
