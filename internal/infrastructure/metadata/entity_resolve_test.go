package metadata

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEntityResolver(t *testing.T) (*EntityResolver, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewEntityResolver(db), mock
}

func artistRow(id, name, mbid, source, ext string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "sort_name", "external_id", "metadata_source", "external_ids", "country", "biography", "cover_image_id", "created_at", "updated_at", "track_count", "roles"}).
		AddRow(id, name, name, mbid, source, ext, "", "", nil, time.Now(), time.Now(), 0, "")
}

func albumRow(id, title, artistID, mbid, source, ext string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "title", "artist_id", "external_id", "metadata_source", "external_ids", "country", "year", "genre", "cover_image_id", "song_count", "duration", "created_at", "updated_at"}).
		AddRow(id, title, artistID, mbid, source, ext, "", 0, "", nil, 0, 0.0, time.Now(), time.Now())
}

func TestFindArtistPrimaryID(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE metadata_source = $1 AND external_id = $2`)).
		WithArgs("netease", "6452").
		WillReturnRows(artistRow("a-1", "周杰伦", "6452", "netease", `{}`))

	a, err := er.FindArtist(context.Background(), "netease", "6452", "周杰伦")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "a-1", a.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindArtistByAlias(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	// primary lookup misses, alias reverse lookup hits
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE metadata_source = $1 AND external_id = $2`)).
		WithArgs("netease", "6452").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE external_ids @> $1::jsonb`)).
		WithArgs([]byte(`{"netease":"6452"}`)).
		WillReturnRows(artistRow("a-1", "Jay Chou", "mbid-1", "musicbrainz", `{"netease":"6452"}`))

	a, err := er.FindArtist(context.Background(), "netease", "6452", "Jay Chou")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "mbid-1", a.ExternalID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindArtistMergesAliasOnNormalizedNameHit(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE metadata_source = $1 AND external_id = $2`)).
		WithArgs("netease", "6452").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE external_ids @> $1::jsonb`)).
		WithArgs([]byte(`{"netease":"6452"}`)).
		WillReturnError(sql.ErrNoRows)
	// normalized name hits an existing musicbrainz artist
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE name_normalized = $1`)).
		WithArgs("周杰伦").
		WillReturnRows(artistRow("a-1", "周杰伦", "mbid-1", "musicbrainz", `{}`))
	// merge writes the netease alias
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE artists SET name=$1, sort_name=$2, external_id=$3, metadata_source=$4, external_ids=$5,
		 name_normalized=$6, country=$7, biography=$8, cover_image_id=$9, updated_at=NOW()
		 WHERE id=$10`)).
		WithArgs("周杰伦", "周杰伦", "mbid-1", "musicbrainz", []byte(`{"netease":"6452"}`), "周杰伦", "", "", nil, "a-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	a, err := er.FindArtist(context.Background(), "netease", "6452", "周杰伦")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "a-1", a.ID)
	assert.Equal(t, "6452", a.ExternalIDs["netease"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindArtistNoMatch(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE metadata_source = $1 AND external_id = $2`)).
		WithArgs("netease", "999").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE external_ids @> $1::jsonb`)).
		WithArgs([]byte(`{"netease":"999"}`)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE name_normalized = $1`)).
		WithArgs("nobody").
		WillReturnError(sql.ErrNoRows)

	a, err := er.FindArtist(context.Background(), "netease", "999", "Nobody")
	require.NoError(t, err)
	assert.Nil(t, a)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindOrCreateArtistCreates(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE metadata_source = $1 AND external_id = $2`)).
		WithArgs("netease", "6452").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE external_ids @> $1::jsonb`)).
		WithArgs([]byte(`{"netease":"6452"}`)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE name_normalized = $1`)).
		WithArgs("周杰伦").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO artists`))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO artists`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	a, err := er.FindOrCreateArtist(context.Background(), "netease", "6452", "周杰伦", "CN")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "netease", a.MetadataSource)
	assert.Equal(t, "6452", a.ExternalID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindAlbumMergesAliasOnNormalizedTitleHit(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE metadata_source = $1 AND external_id = $2`)).
		WithArgs("netease", "216297").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE external_ids @> $1::jsonb`)).
		WithArgs([]byte(`{"netease":"216297"}`)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE title_normalized = $1 AND artist_id = $2`)).
		WithArgs("十一月的萧邦", "a-1").
		WillReturnRows(albumRow("alb-1", "十一月的萧邦", "a-1", "mbid-alb", "musicbrainz", `{}`))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE albums SET title=$1, artist_id=$2, external_id=$3, metadata_source=$4, external_ids=$5,
		 title_normalized=$6, country=$7, year=$8, genre=$9,
		 cover_image_id=$10, song_count=$11, duration=$12, updated_at=NOW()
		 WHERE id=$13`)).
		WithArgs("十一月的萧邦", "a-1", "mbid-alb", "musicbrainz", []byte(`{"netease":"216297"}`), "十一月的萧邦", "", 0, "", nil, 0, 0.0, "alb-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	a, err := er.FindAlbum(context.Background(), "netease", "216297", "十一月的萧邦", "a-1")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "216297", a.ExternalIDs["netease"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindOrCreateAlbumCreates(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE metadata_source = $1 AND external_id = $2`)).
		WithArgs("netease", "216297").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE external_ids @> $1::jsonb`)).
		WithArgs([]byte(`{"netease":"216297"}`)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE title_normalized = $1 AND artist_id = $2`)).
		WithArgs("十一月的萧邦", "a-1").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectBegin()
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO albums`))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO albums`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	a, err := er.FindOrCreateAlbum(context.Background(), "netease", "216297", "十一月的萧邦", "a-1", 2005, "", "")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "netease", a.MetadataSource)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindArtistPropagatesRealErrors(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	// A real DB failure (not sql.ErrNoRows) must surface, not fall through.
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE metadata_source = $1 AND external_id = $2`)).
		WithArgs("netease", "6452").
		WillReturnError(errors.New("connection reset"))

	_, err := er.FindArtist(context.Background(), "netease", "6452", "周杰伦")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindAlbumPropagatesRealErrors(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE metadata_source = $1 AND external_id = $2`)).
		WithArgs("netease", "216297").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE external_ids @> $1::jsonb`)).
		WithArgs([]byte(`{"netease":"216297"}`)).
		WillReturnError(errors.New("connection reset"))

	_, err := er.FindAlbum(context.Background(), "netease", "216297", "十一月的萧邦", "a-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindOrCreateArtistEmptyNameReusesUnknown(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	// Empty name falls back to "Unknown Artist"; the normalized lookup hits
	// the existing placeholder so nothing is inserted.
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE name_normalized = $1`)).
		WithArgs("unknownartist").
		WillReturnRows(artistRow("a-unk", "Unknown Artist", "", "musicbrainz", `{}`))

	a, err := er.FindOrCreateArtist(context.Background(), "netease", "", "", "")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "a-unk", a.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindOrCreateAlbumEmptyTitleReusesUnknown(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	// Empty title falls back to "Unknown Album"; normalized lookup within the
	// artist hits the existing placeholder.
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE title_normalized = $1 AND artist_id = $2`)).
		WithArgs("unknownalbum", "a-1").
		WillReturnRows(albumRow("alb-unk", "Unknown Album", "a-1", "", "musicbrainz", `{}`))

	a, err := er.FindOrCreateAlbum(context.Background(), "netease", "", "", "a-1", 0, "", "")
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, "alb-unk", a.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindAlbumGuardsEmptyTitle(t *testing.T) {
	er, mock := newTestEntityResolver(t)
	// external ID missing and title empty → nothing is queried
	a, err := er.FindAlbum(context.Background(), "netease", "", "", "a-1")
	require.NoError(t, err)
	assert.Nil(t, a)
	require.NoError(t, mock.ExpectationsWereMet())
}
