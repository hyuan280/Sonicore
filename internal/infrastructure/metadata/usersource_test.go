package metadata

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

func newUserSource(t *testing.T, row *repository.UserMetadata) *userSource {
	t.Helper()
	db, mock := newMockDB(t)
	repo := repository.NewUserMetadataRepo(db)
	if row != nil {
		mock.ExpectQuery("FROM user_metadata WHERE user_id = \\$1 AND file_hash = \\$2").
			WithArgs(row.UserID, row.FileHash).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "file_hash", "metadata_source", "external_id", "title", "artist", "album", "album_artist",
				"track_number", "disc_number", "year", "genre"}).
				AddRow(row.UserID, row.FileHash, row.MetadataSource, row.ExternalID, row.Title, row.Artist, row.Album, row.AlbumArtist,
					row.TrackNumber, row.DiscNumber, row.Year, row.Genre))
	} else {
		mock.ExpectQuery("FROM user_metadata WHERE user_id = \\$1 AND file_hash = \\$2").
			WillReturnError(sql.ErrNoRows)
	}
	return NewUserSource(repo)
}

func TestUserSourcePriorityAndCapabilities(t *testing.T) {
	db, _ := newMockDB(t)
	s := NewUserSource(repository.NewUserMetadataRepo(db))
	assert.Equal(t, "user", s.Name())
	assert.True(t, s.Enabled())
	assert.Equal(t, 0, s.Priority(), "user source runs first")
	caps := s.Capabilities()
	assert.NotEqual(t, port.MetadataFields(0), caps&port.FieldTitle)
	assert.NotEqual(t, port.MetadataFields(0), caps&port.FieldYear)
	assert.Equal(t, port.MetadataFields(0), caps&port.FieldCoverURL, "no network cover")
	assert.Equal(t, port.MetadataFields(0), caps&port.FieldLyrics, "no lyrics")
}

func TestUserSourceNilRepoDisabled(t *testing.T) {
	s := NewUserSource(nil)
	assert.False(t, s.Enabled())
	assert.Equal(t, 0, len(NewRegistry(s).Sources()))
}

func TestUserSourceIdentifyHit(t *testing.T) {
	row := &repository.UserMetadata{
		UserID:         "u-1",
		FileHash:       "h1",
		MetadataSource: "musicbrainz",
		ExternalID:     "mbid-9",
		Title:          "用户标题",
		Artist:         "用户艺人",
		Album:          "用户专辑",
		Year:           2020,
		Genre:          "Rock",
	}
	s := newUserSource(t, row)

	c, err := s.Identify(context.Background(), port.MetadataQuery{UserID: "u-1", FileHash: "h1"})
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "musicbrainz", c.Source, "source recorded in the cache")
	assert.Equal(t, "mbid-9", c.ExternalID, "external id recorded in the cache")
	assert.Equal(t, "用户标题", c.Title)
	require.Len(t, c.Artists, 1)
	assert.Equal(t, "用户艺人", c.Artists[0].Name)
	assert.Equal(t, 2020, c.Year)
	assert.Equal(t, 1.0, c.Score)
}

func TestUserSourceIdentifyMiss(t *testing.T) {
	s := newUserSource(t, nil)
	c, err := s.Identify(context.Background(), port.MetadataQuery{UserID: "u-1", FileHash: "nope"})
	require.NoError(t, err)
	assert.Nil(t, c)
}

func TestUserSourceIdentifyNeedsLocators(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewUserSource(repository.NewUserMetadataRepo(db))

	c, err := s.Identify(context.Background(), port.MetadataQuery{Title: "Song"})
	require.NoError(t, err)
	assert.Nil(t, c, "no file hash / user id → no lookup")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserSourceNoSearchOrLookup(t *testing.T) {
	db, _ := newMockDB(t)
	s := NewUserSource(repository.NewUserMetadataRepo(db))

	cs, err := s.SearchCandidates(context.Background(), port.MetadataQuery{Title: "X"})
	require.NoError(t, err)
	assert.Nil(t, cs)

	c, err := s.Lookup(context.Background(), "any")
	require.NoError(t, err)
	assert.Nil(t, c)
}

func newMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, mock
}

func TestUserSourceInRegistryChain(t *testing.T) {
	db, mock := newMockDB(t)
	row := &repository.UserMetadata{
		UserID:   "u-1",
		FileHash: "h1",
		Title:    "用户标题",
		Artist:   "用户艺人",
		Album:    "用户专辑",
		Year:     2020,
	}
	mock.ExpectQuery("FROM user_metadata WHERE user_id = \\$1 AND file_hash = \\$2").
		WithArgs("u-1", "h1").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "file_hash", "metadata_source", "external_id", "title", "artist", "album", "album_artist",
			"track_number", "disc_number", "year", "genre"}).
			AddRow(row.UserID, row.FileHash, "netease", "ne-9", row.Title, row.Artist, row.Album, "",
				0, 0, row.Year, ""))

	ne := &fakeSource{name: "ne", enabled: true, priority: 20,
		caps: port.FieldTrackID | port.FieldTitle | port.FieldArtists | port.FieldAlbum | port.FieldCoverURL | port.FieldLyrics}
	ne.identify = func(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
		c := fullCand("ne", "ne-9", "用户标题")
		// Same song: the candidate must share an artist with the cached
		// identity, or the equal-title-only rule rejects it.
		c.Artists = []port.ArtistInfo{{Name: "用户艺人"}}
		return c, nil
	}

	reg := NewRegistry(NewUserSource(repository.NewUserMetadataRepo(db)), ne)
	got, err := reg.Identify(context.Background(), port.MetadataQuery{Title: "x", UserID: "u-1", FileHash: "h1"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "netease", got.Source, "source from the cache")
	assert.Equal(t, "ne-9", got.ExternalID, "external id from the cache")
	assert.Equal(t, "用户标题", got.Title, "user title authoritative")
	assert.Equal(t, "https://cover", got.CoverArtURL, "cover still completed by netease")
	assert.Equal(t, "lyrics", got.Lyrics, "lyrics still completed by netease")
}

// newMockDB duplicate guard: keep a single definition even if another test
// file already provides one.
var _ = errors.New
