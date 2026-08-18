package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageRepoSharedPaths(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewImageRepo(db)
	q := regexp.QuoteMeta(`SELECT DISTINCT path FROM images WHERE path = ANY($1) AND id != ALL($2)`)
	mock.ExpectQuery(q).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"path"}).
			AddRow("/img/lib-1/track_live-1.jpg"))

	got, err := repo.SharedPaths(context.Background(), []string{
		"/img/lib-1/track_live-1.jpg",
		"/img/lib-1/track_gone-1_64.jpg",
	}, []string{"img-orphan-1", "img-orphan-2"})
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"/img/lib-1/track_live-1.jpg": {}}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageRepoSharedPathsEmpty(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewImageRepo(db)
	got, err := repo.SharedPaths(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet(), "no query for an empty path set")
}

func TestImageRepoFindOrphans(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewImageRepo(db)
	now := time.Date(2024, 10, 1, 20, 0, 0, 0, time.UTC)
	q := regexp.QuoteMeta(`SELECT id, library_id, owner_type, owner_id, source, path,
		 format, width, height, size, hash, variants, created_at, updated_at
		 FROM images
		 WHERE (owner_type = 'track' AND NOT EXISTS (SELECT 1 FROM tracks t WHERE t.id = images.owner_id))
		    OR (owner_type = 'album' AND NOT EXISTS (SELECT 1 FROM albums a WHERE a.id = images.owner_id))
		    OR (owner_type = 'artist' AND NOT EXISTS (SELECT 1 FROM artists ar WHERE ar.id = images.owner_id))`)

	mock.ExpectQuery(q).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "library_id", "owner_type", "owner_id", "source", "path",
			"format", "width", "height", "size", "hash", "variants", "created_at", "updated_at",
		}).
			AddRow("img-1", "lib-1", "track", "gone-track-1", "embed", "/data/images/lib-1/track_gone-track-1.jpg", "jpg", 600, 600, 1234, "h1", "[]", now, now).
			AddRow("img-2", nil, "album", "gone-album-1", "backfill", "/data/images/album/album_gone-album-1_256.jpg", "jpg", 256, 256, 321, "h2", "[]", now, now))

	got, err := repo.FindOrphans(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "img-1", got[0].ID)
	assert.Equal(t, "gone-track-1", got[0].OwnerID)
	assert.Equal(t, "gone-album-1", got[1].OwnerID)
	assert.Equal(t, "", got[1].LibraryID)
	require.NoError(t, mock.ExpectationsWereMet())
}
