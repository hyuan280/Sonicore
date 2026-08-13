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
	"github.com/sonicore/server/internal/config"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	return NewCoverHandler(db, imagesDir, cache.NewSessionStore(vk)), mock, mr, imagesDir
}

func coverSession(t *testing.T, h *CoverHandler) string {
	t.Helper()
	sess, err := h.sessionStore.Generate(context.Background(), "u-001", "cover-client")
	require.NoError(t, err)
	return sess
}

func coverRequest(session, ownerType, ownerID string) *http.Request {
	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/covers/"+session+"/"+ownerType+"/"+ownerID, nil),
		map[string]string{"session": session, "ownerType": ownerType, "ownerId": ownerID})
	return req
}

func TestCoverMissingSession(t *testing.T) {
	h, _, _, _ := newCoverHandler(t)

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest("", "album", "alb-1"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing session")
}

func TestCoverInvalidSession(t *testing.T) {
	h, _, _, _ := newCoverHandler(t)

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest("bogus", "album", "alb-1"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCoverMissingOwnerParams(t *testing.T) {
	h, _, mr, _ := newCoverHandler(t)
	sess := coverSession(t, h)

	rec := httptest.NewRecorder()
	h.Serve(rec, mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"session": sess, "ownerType": "", "ownerId": ""}))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	_ = mr
}

func TestCoverTrackNotFound(t *testing.T) {
	h, mock, _, _ := newCoverHandler(t)
	sess := coverSession(t, h)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest(sess, "track", "missing"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "cover not found")
}

func TestCoverForbidden(t *testing.T) {
	h, mock, _, _ := newCoverHandler(t)
	sess := coverSession(t, h)

	expectStreamTrack(mock, streamTestTrack())
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest(sess, "track", "t-001"))

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCoverServesTrackImage(t *testing.T) {
	h, mock, _, imagesDir := newCoverHandler(t)
	sess := coverSession(t, h)

	// create the thumbnail file the handler looks for (64px variant first)
	thumbPath := filepath.Join(imagesDir, "lib-001", "track_t-001_64.jpg")
	require.NoError(t, os.MkdirAll(filepath.Dir(thumbPath), 0755))
	require.NoError(t, os.WriteFile(thumbPath, []byte("jpeg-data"), 0644))

	// track with CoverImageID set, owner = u-001
	rows := sqlmock.NewRows([]string{"id", "library_id", "title", "cover_image_id",
		"duration", "bit_rate", "sample_rate", "channels",
		"file_path", "file_size", "file_format", "audio_codec", "mbid", "metadata_source", "acoust_id", "hash",
		"lyrics_mask", "lyrics_offset", "heat", "play_count", "last_played_at", "metadata", "version", "version_label", "created_at", "updated_at"}).
		AddRow("t-001", "lib-001", "Song", "t-001", 200, 128000, 44100, 2, "/m/song.mp3", 8, "mp3", "mp3", "", "musicbrainz", "", "h",
			0, 0, 0, 0, nil, nil, 1, "", time.Now(), time.Now())
	expectStreamTrack(mock, rows)
	// IsMember → IsOwner
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest(sess, "track", "t-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "jpeg-data", rec.Body.String())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCoverFallsBackToAlbumImage(t *testing.T) {
	h, mock, _, imagesDir := newCoverHandler(t)
	sess := coverSession(t, h)

	// only the album-level image exists (album fallback uses "album" as dir)
	albumImg := filepath.Join(imagesDir, "album", "album_alb-1_64.jpg")
	require.NoError(t, os.MkdirAll(filepath.Dir(albumImg), 0755))
	require.NoError(t, os.WriteFile(albumImg, []byte("album-jpeg"), 0644))

	// track WITHOUT cover but with album relation
	rows := sqlmock.NewRows([]string{"id", "library_id", "title", "cover_image_id",
		"duration", "bit_rate", "sample_rate", "channels",
		"file_path", "file_size", "file_format", "audio_codec", "mbid", "metadata_source", "acoust_id", "hash",
		"lyrics_mask", "lyrics_offset", "heat", "play_count", "last_played_at", "metadata", "version", "version_label", "created_at", "updated_at"}).
		AddRow("t-001", "lib-001", "Song", nil, 200, 128000, 44100, 2, "/m/song.mp3", 8, "mp3", "mp3", "", "musicbrainz", "", "h",
			0, 0, 0, 0, nil, nil, 1, "", time.Now(), time.Now())
	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnRows(rows)
	// albums query returns one album
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_albums ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "album_id", "track_number", "disc_number", "title", "cover_image_id"}).
			AddRow("t-001", "alb-1", 1, 1, "Album", "alb-1"))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "mbid"}))
	// IsMember → IsOwner
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))
	// albumRepo.FindByID
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE id = $1`)).
		WithArgs("alb-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "artist_id", "mbid", "metadata_source", "external_ids", "country", "year", "genre", "cover_image_id", "song_count", "duration", "created_at", "updated_at"}).
			AddRow("alb-1", "Album", "art-1", "", "musicbrainz", `{}`, "", 0, "", "alb-1", 0, 0.0, time.Now(), time.Now()))

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest(sess, "track", "t-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "album-jpeg", rec.Body.String(), "falls back to album cover")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCoverNoImageFound(t *testing.T) {
	h, mock, _, _ := newCoverHandler(t)
	sess := coverSession(t, h)

	expectStreamTrack(mock, streamTestTrack())
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))

	rec := httptest.NewRecorder()
	h.Serve(rec, coverRequest(sess, "artist", "art-1"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
