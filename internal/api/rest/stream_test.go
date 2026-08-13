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

func newStreamHandler(t *testing.T) (*StreamHandler, sqlmock.Sqlmock, *miniredis.Miniredis) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	mr := miniredis.RunT(t)
	vk := cache.NewValkey(config.RedisConfig{Host: mr.Host(), Port: mr.Server().Addr().Port, KeyPrefix: "test:"})
	t.Cleanup(func() { vk.Close() })

	return NewStreamHandler(db, cache.NewSessionStore(vk)), mock, mr
}

func streamTestTrack() *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows([]string{"id", "library_id", "title", "cover_image_id",
		"duration", "bit_rate", "sample_rate", "channels",
		"file_path", "file_size", "file_format", "audio_codec", "mbid", "metadata_source", "acoust_id", "hash",
		"lyrics_mask", "lyrics_offset", "heat", "play_count", "last_played_at", "metadata", "version", "version_label", "created_at", "updated_at"}).
		AddRow("t-001", "lib-001", "Song", nil, 200, 128000, 44100, 2, "/m/song.mp3", 12345, "mp3", "mp3", "", "musicbrainz", "", "h",
			0, 0, 0, 0, nil, nil, 1, "", now, now)
}

// expectStreamTrack mocks TrackRepo.FindByID (main + albums + artists).
func expectStreamTrack(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnRows(rows)
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_albums ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "album_id", "track_number", "disc_number", "title", "cover_image_id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "mbid"}))
}

func validSession(t *testing.T, h *StreamHandler) string {
	t.Helper()
	sess, err := h.sessionStore.Generate(context.Background(), "u-001", "stream-client")
	require.NoError(t, err)
	return sess
}

func streamRequest(path string, sess string) *http.Request {
	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, path, nil),
		map[string]string{"session": sess, "id": "t-001"})
	return req
}

func TestStreamMissingSession(t *testing.T) {
	h, _, _ := newStreamHandler(t)

	rec := httptest.NewRecorder()
	h.ServeStream(rec, streamRequest("/api/stream//t-001", ""))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing session")
}

func TestStreamInvalidSession(t *testing.T) {
	h, _, _ := newStreamHandler(t)

	rec := httptest.NewRecorder()
	h.ServeStream(rec, streamRequest("/api/stream/bogus/t-001", "bogus"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid session")
}

func TestStreamTrackNotFound(t *testing.T) {
	h, mock, _ := newStreamHandler(t)
	sess := validSession(t, h)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.ServeStream(rec, streamRequest("/api/stream/"+sess+"/t-001", sess))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestStreamForbidden(t *testing.T) {
	h, mock, _ := newStreamHandler(t)
	sess := validSession(t, h)

	expectStreamTrack(mock, streamTestTrack())
	// permission: owner check fails (not owner)
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.ServeStream(rec, streamRequest("/api/stream/"+sess+"/t-001", sess))

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestStreamServesFileDirectly(t *testing.T) {
	h, mock, _ := newStreamHandler(t)
	sess := validSession(t, h)

	audioFile := filepath.Join(t.TempDir(), "song.mp3")
	require.NoError(t, os.WriteFile(audioFile, []byte("fake-mp3"), 0644))

	rows := streamTestTrack()
	// point the mock track at the real temp file
	rows = sqlmock.NewRows([]string{"id", "library_id", "title", "cover_image_id",
		"duration", "bit_rate", "sample_rate", "channels",
		"file_path", "file_size", "file_format", "audio_codec", "mbid", "metadata_source", "acoust_id", "hash",
		"lyrics_mask", "lyrics_offset", "heat", "play_count", "last_played_at", "metadata", "version", "version_label", "created_at", "updated_at"}).
		AddRow("t-001", "lib-001", "Song", nil, 200, 128000, 44100, 2, audioFile, 8, "mp3", "mp3", "", "musicbrainz", "", "h",
			0, 0, 0, 0, nil, nil, 1, "", time.Now(), time.Now())
	expectStreamTrack(mock, rows)
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))

	rec := httptest.NewRecorder()
	h.ServeStream(rec, streamRequest("/api/stream/"+sess+"/t-001", sess))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "audio/mp3", rec.Header().Get("Content-Type"))
	assert.Equal(t, "8", rec.Header().Get("Content-Length"))
	assert.Equal(t, "bytes", rec.Header().Get("Accept-Ranges"))
	assert.Equal(t, "fake-mp3", rec.Body.String())
}

func TestStreamTranscodeStatus(t *testing.T) {
	h, mock, _ := newStreamHandler(t)
	sess := validSession(t, h)

	expectStreamTrack(mock, streamTestTrack())
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))

	rec := httptest.NewRecorder()
	h.ServeTranscodeStatus(rec, streamRequest("/api/stream/"+sess+"/t-001/transcode-status", sess))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"ready":false`, "no transcode cache configured")
	require.NoError(t, mock.ExpectationsWereMet())
}
