package rest

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/config"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/sonicore/server/internal/infrastructure/metadata"
)

func newCoverHandler(t *testing.T) (*CoverHandler, sqlmock.Sqlmock, *miniredis.Miniredis, string) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	mr := miniredis.RunT(t)
	vk := cache.NewValkey(config.RedisConfig{Host: mr.Host(), Port: mr.Server().Addr().Port, KeyPrefix: "test:"})
	t.Cleanup(func() { vk.Close() })

	imagesDir := t.TempDir()
	return NewCoverHandler(db, imagesDir, cache.NewSessionStore(vk), metadata.NewCoverManager(imagesDir, db, nil)), mock, mr, imagesDir
}

func coverSession(t *testing.T, h *CoverHandler) string {
	t.Helper()
	sess, err := h.sessionStore.Generate(context.Background(), "u-001", "cover-client")
	require.NoError(t, err)
	return sess
}

func coverRequest(session, imageID string) *http.Request {
	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/covers/"+session+"/"+imageID, nil),
		map[string]string{"session": session, "imageId": imageID})
	return req
}

func imageRows(imgID, libraryID, ownerType, ownerID, path, variants string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "library_id", "owner_type", "owner_id", "source", "path",
		"format", "width", "height", "size", "hash", "variants", "created_at", "updated_at"}).
		AddRow(imgID, libraryID, ownerType, ownerID, "embedded", path, "jpg", 800, 800, 1234, "h", variants, time.Now(), time.Now())
}

func TestCoverMissingSession(t *testing.T) {
	h, _, _, _ := newCoverHandler(t)

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest("", "img-1"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing session")
}

func TestCoverInvalidSession(t *testing.T) {
	h, _, _, _ := newCoverHandler(t)

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest("bogus", "img-1"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCoverMissingImageID(t *testing.T) {
	h, _, mr, _ := newCoverHandler(t)
	sess := coverSession(t, h)

	rec := httptest.NewRecorder()
	h.Serve(rec, mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"session": sess, "imageId": ""}))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	_ = mr
}

func TestCoverUnknownImage404(t *testing.T) {
	h, mock, _, _ := newCoverHandler(t)
	sess := coverSession(t, h)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM images WHERE id = $1`)).
		WithArgs("ghost").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest(sess, "ghost"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "cover not found")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCoverForbiddenForForeignLibrary(t *testing.T) {
	h, mock, _, _ := newCoverHandler(t)
	sess := coverSession(t, h)

	// image belongs to lib-999, user u-001 is not a member
	mock.ExpectQuery(regexp.QuoteMeta(`FROM images WHERE id = $1`)).
		WithArgs("img-1").
		WillReturnRows(imageRows("img-1", "lib-999", "track", "t-1", "/x.jpg", "[]"))
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-999").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest(sess, "img-1"))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCoverServesTrackImage(t *testing.T) {
	h, mock, _, imagesDir := newCoverHandler(t)
	sess := coverSession(t, h)

	// The images row points at a real file (original); the 64 variant fits
	// the default size request.
	mainPath := filepath.Join(imagesDir, "lib-001", "track_t-001.jpg")
	require.NoError(t, os.MkdirAll(filepath.Dir(mainPath), 0755))
	require.NoError(t, os.WriteFile(mainPath, []byte("jpeg-data"), 0644))

	mock.ExpectQuery(regexp.QuoteMeta(`FROM images WHERE id = $1`)).
		WithArgs("img-1").
		WillReturnRows(imageRows("img-1", "lib-001", "track", "t-001", mainPath,
			`[{"path":"`+mainPath+`","width":800,"height":800,"size":1234}]`))
	// IsMember → IsOwner
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest(sess, "img-1"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "jpeg-data", rec.Body.String())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCoverMissingFileTriesOnDemandThen404(t *testing.T) {
	h, mock, _, imagesDir := newCoverHandler(t)
	sess := coverSession(t, h)

	// images row exists but its file was deleted; the on-demand extraction
	// fails (no real audio file), so the request ends in 404.
	mainPath := filepath.Join(imagesDir, "lib-001", "track_t-001.jpg")

	mock.ExpectQuery(regexp.QuoteMeta(`FROM images WHERE id = $1`)).
		WithArgs("img-1").
		WillReturnRows(imageRows("img-1", "lib-001", "track", "t-001", mainPath, "[]"))
	// IsMember → IsOwner
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))
	// On-demand extraction path: trackRepo.FindByID loads track + relations
	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "library_id", "title", "cover_image_id",
			"duration", "bit_rate", "sample_rate", "channels",
			"file_path", "file_size", "file_format", "audio_codec", "external_id", "metadata_source", "external_ids", "acoust_id", "hash",
			"lyrics_mask", "lyrics_offset", "heat", "play_count", "last_played_at", "metadata", "version", "version_label", "created_at", "updated_at"}).
			AddRow("t-001", "lib-001", "Song", "img-1", 200, 128000, 44100, 2, "/m/song.mp3", 8, "mp3", "mp3", "", "musicbrainz", "{}", "", "h",
				0, 0, 0, 0, nil, nil, 1, "", time.Now(), time.Now()))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_albums ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "album_id", "track_number", "disc_number", "title", "cover_image_id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "external_id", "metadata_source"}))

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest(sess, "img-1"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
