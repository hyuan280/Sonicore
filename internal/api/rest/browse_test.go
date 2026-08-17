package rest

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePagination(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?page=2&per_page=25", nil)
	page, perPage := parsePagination(r)
	assert.Equal(t, 2, page)
	assert.Equal(t, 25, perPage)

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	page, perPage = parsePagination(r)
	assert.Equal(t, 1, page, "default page 1")
	assert.Equal(t, 10, perPage, "default per_page 10")

	r = httptest.NewRequest(http.MethodGet, "/?page=0&per_page=0", nil)
	page, perPage = parsePagination(r)
	assert.Equal(t, 1, page, "page < 1 clamps")
	assert.Equal(t, 0, perPage, "per_page < 1 becomes 0 (signals no pagination)")

	r = httptest.NewRequest(http.MethodGet, "/?page=-5&per_page=500", nil)
	page, perPage = parsePagination(r)
	assert.Equal(t, 1, page)
	assert.Equal(t, 100, perPage, "per_page > 100 clamps")

	r = httptest.NewRequest(http.MethodGet, "/?page=abc&per_page=xyz", nil)
	page, perPage = parsePagination(r)
	assert.Equal(t, 1, page)
	assert.Equal(t, 0, perPage, "invalid per_page parses to 0")
}

func TestDataTracksZeroPerPage(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	h := NewDataHandler(db)
	rec := httptest.NewRecorder()
	h.Tracks(rec, httptest.NewRequest(http.MethodGet, "/api/data/tracks?per_page=0", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"items":[]`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDataTracksForbidden(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	h := NewDataHandler(db)
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodGet, "/api/data/tracks?libId=lib-001", nil)
	rec := httptest.NewRecorder()
	h.Tracks(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "access denied")
}

func TestDataTracksByLibrary(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	h := NewDataHandler(db)

	// permission: IsMember → IsOwner
	mock.ExpectQuery(`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = \$1`).
		WithArgs("lib-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))

	// FindByLibraryID
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE library_id = ANY($1)`)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "library_id", "title", "cover_image_id",
			"duration", "bit_rate", "sample_rate", "channels",
			"file_path", "file_size", "file_format", "audio_codec", "external_id", "metadata_source", "external_ids", "acoust_id", "hash",
			"lyrics_mask", "lyrics_offset", "heat", "play_count", "last_played_at", "metadata", "version", "version_label", "created_at", "updated_at"}).
			AddRow("t-001", "lib-001", "Song A", nil, 200, 128000, 44100, 2, "/m/a.mp3", 8, "mp3", "mp3", "", "musicbrainz", "{}", "", "h",
				0, 0, 0, 0, nil, nil, 1, "", now, now).
			AddRow("t-002", "lib-001", "Song B", nil, 200, 128000, 44100, 2, "/m/b.mp3", 8, "mp3", "mp3", "", "musicbrainz", "{}", "", "h",
				0, 0, 0, 0, nil, nil, 1, "", now, now))

	req := httptest.NewRequest(http.MethodGet, "/api/data/tracks?libId=lib-001&q=song", nil)
	rec := httptest.NewRecorder()
	h.Tracks(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"title":"Song A"`)
	assert.Contains(t, rec.Body.String(), `"title":"Song B"`)
	assert.Contains(t, rec.Body.String(), `"total":2`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDataTracksSearchFilter(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	h := NewDataHandler(db)

	// no libId → FindByUserID first
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE lm.user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE library_id = ANY($1)`)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "library_id", "title", "cover_image_id",
			"duration", "bit_rate", "sample_rate", "channels",
			"file_path", "file_size", "file_format", "audio_codec", "external_id", "metadata_source", "external_ids", "acoust_id", "hash",
			"lyrics_mask", "lyrics_offset", "heat", "play_count", "last_played_at", "metadata", "version", "version_label", "created_at", "updated_at"}).
			AddRow("t-001", "lib-001", "Match Song", nil, 200, 128000, 44100, 2, "/m/a.mp3", 8, "mp3", "mp3", "", "musicbrainz", "{}", "", "h",
				0, 0, 0, 0, nil, nil, 1, "", now, now).
			AddRow("t-002", "lib-001", "Unrelated", nil, 200, 128000, 44100, 2, "/m/b.mp3", 8, "mp3", "mp3", "", "musicbrainz", "{}", "", "h",
				0, 0, 0, 0, nil, nil, 1, "", now, now).
			AddRow("t-003", "lib-001", "Version 2 Track", nil, 200, 128000, 44100, 2, "/m/c.mp3", 8, "mp3", "mp3", "", "musicbrainz", "{}", "", "h",
				0, 0, 0, 0, nil, nil, 2, "", now, now))

	req := httptest.NewRequest(http.MethodGet, "/api/data/tracks?q=song", nil)
	rec := httptest.NewRecorder()
	h.Tracks(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"title":"Match Song"`)
	assert.NotContains(t, rec.Body.String(), "Unrelated", "query filters")
	assert.NotContains(t, rec.Body.String(), "Version 2", "version > 1 hidden by default")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDataTracksShowAllVersions(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	h := NewDataHandler(db)

	// no libId → FindByUserID returns empty list → no library ids
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE lm.user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}))

	// FindByLibraryID with no ids → returns nil early (no query)
	req := httptest.NewRequest(http.MethodGet, "/api/data/tracks?all=1", nil)
	rec := httptest.NewRecorder()
	h.Tracks(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}
