package rest

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newUserDataHandler(t *testing.T) (*UserDataHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewUserDataHandler(db), mock
}

func udRequest(method, path, body, userID string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if userID != "" {
		req = req.WithContext(contextWithUserID(req.Context(), userID))
	}
	return req
}

// ---- Favorites ----

func TestListFavoritesUnauthorized(t *testing.T) {
	h, _ := newUserDataHandler(t)

	rec := httptest.NewRecorder()
	h.ListFavorites(rec, httptest.NewRequest(http.MethodGet, "/api/user/favorites", nil))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListFavoritesZeroPerPage(t *testing.T) {
	h, mock := newUserDataHandler(t)

	rec := httptest.NewRecorder()
	h.ListFavorites(rec, udRequest(http.MethodGet, "/api/user/favorites?per_page=0", "", "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"items":[]`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListFavoritesAlbumType(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM favorites WHERE user_id = $1 AND ($2 = '' OR item_type = $2) ORDER BY created_at DESC`)).
		WithArgs("u-001", "album").
		WillReturnRows(sqlmock.NewRows([]string{"item_type", "item_id", "created_at"}).
			AddRow("album", "alb-1", time.Now()).
			AddRow("album", "alb-2", time.Now()))

	rec := httptest.NewRecorder()
	h.ListFavorites(rec, udRequest(http.MethodGet, "/api/user/favorites?type=album", "", "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"item_id":"alb-1"`)
	assert.Contains(t, rec.Body.String(), `"item_id":"alb-2"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListFavoritesTrackType(t *testing.T) {
	h, mock := newUserDataHandler(t)

	// COUNT query
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(DISTINCT CASE`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	// main query
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT DISTINCT ON`)).
		WithArgs("u-001", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"item_type", "item_id", "created_at", "title", "album_title", "album_id", "duration", "file_format", "cover_image_id", "version", "version_label", "external_id", "metadata_source"}).
			AddRow("track", "t-001", time.Now(), "Song", "Album", "alb-1", 200.0, "mp3", nil, 1, "", "", "musicbrainz"))
	// loadTrackArtistsBulk
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "external_id", "metadata_source"}).
			AddRow("t-001", "art-1", "performer", 0, "Band", "", "musicbrainz"))

	rec := httptest.NewRecorder()
	h.ListFavorites(rec, udRequest(http.MethodGet, "/api/user/favorites?type=track", "", "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"title":"Song"`)
	assert.Contains(t, rec.Body.String(), `"artists"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddFavoritesSuccess(t *testing.T) {
	h, mock := newUserDataHandler(t)

	// expandTrackVersions: FindByUserID
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE lm.user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))
	// track has no external id → passes through
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT external_id, metadata_source FROM tracks WHERE id = $1 AND external_id != '' AND library_id = ANY($2)`)).
		WithArgs("t-001", sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	// library_id lookup
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT library_id FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"library_id"}).AddRow("lib-001"))
	// INSERT favorite
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO favorites`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rec := httptest.NewRecorder()
	h.AddFavorites(rec, udRequest(http.MethodPost, "/api/user/favorites",
		`{"item_type":"track","item_ids":["t-001"]}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "favorited")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddFavoritesExpandsVersions(t *testing.T) {
	h, mock := newUserDataHandler(t)

	// expandTrackVersions: FindByUserID
	mock.ExpectQuery(regexp.QuoteMeta(`WHERE lm.user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))
	// track has external id
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT external_id, metadata_source FROM tracks WHERE id = $1 AND external_id != '' AND library_id = ANY($2)`)).
		WithArgs("t-001", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"external_id", "metadata_source"}).AddRow("mbid-1", "musicbrainz"))
	// expand: SELECT ids with same external id → t-001 + t-002
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id FROM tracks WHERE external_id = $1 AND metadata_source = $2 AND library_id = ANY($3)`)).
		WithArgs("mbid-1", "musicbrainz", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("t-001").AddRow("t-002"))

	// two library lookups + two inserts
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT library_id FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"library_id"}).AddRow("lib-001"))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO favorites`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT library_id FROM tracks WHERE id = $1`)).
		WithArgs("t-002").
		WillReturnRows(sqlmock.NewRows([]string{"library_id"}).AddRow("lib-001"))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO favorites`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rec := httptest.NewRecorder()
	h.AddFavorites(rec, udRequest(http.MethodPost, "/api/user/favorites",
		`{"item_type":"track","item_ids":["t-001"]}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRemoveFavorites(t *testing.T) {
	h, mock := newUserDataHandler(t)

	// non-track item type → expandTrackVersions passes through
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM favorites WHERE user_id = $1 AND item_type = $2 AND item_id = $3`)).
		WithArgs("u-001", "album", "alb-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec := httptest.NewRecorder()
	h.RemoveFavorites(rec, udRequest(http.MethodPost, "/api/user/favorites/remove",
		`{"item_type":"album","item_ids":["alb-1"]}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "removed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckFavorites(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT item_id FROM favorites WHERE user_id = $1 AND item_type = 'track' AND item_id = ANY($2)`)).
		WithArgs("u-001", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"item_id"}).AddRow("t-001"))

	rec := httptest.NewRecorder()
	h.CheckFavorites(rec, udRequest(http.MethodPost, "/api/user/favorites/check",
		`{"ids":["t-001","t-002"]}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"t-001":true`)
	assert.NotContains(t, rec.Body.String(), "t-002", "not-favorited ids are absent from the map")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckFavoritesEmpty(t *testing.T) {
	h, _ := newUserDataHandler(t)

	rec := httptest.NewRecorder()
	h.CheckFavorites(rec, udRequest(http.MethodPost, "/api/user/favorites/check", `{"ids":[]}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"favorites":{}`)
}

// ---- History ----

func TestListHistory(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM play_history WHERE user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT ph.id, ph.track_id, ph.played_at`)).
		WithArgs("u-001", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "track_id", "played_at", "title", "album_title", "album_id", "duration", "file_format", "cover_image_id"}).
			AddRow("h-1", "t-001", time.Now(), "Song", "Album", "alb-1", 200.0, "mp3", nil))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "external_id", "metadata_source"}))

	rec := httptest.NewRecorder()
	h.ListHistory(rec, udRequest(http.MethodGet, "/api/user/history", "", "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"title":"Song"`)
	assert.Contains(t, rec.Body.String(), `"total":1`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddHistory(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM play_history WHERE user_id=$1 AND track_id=$2`)).
		WithArgs("u-001", "t-001").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT library_id FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"library_id"}).AddRow("lib-001"))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO play_history`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rec := httptest.NewRecorder()
	h.AddHistory(rec, udRequest(http.MethodPost, "/api/user/history",
		`{"track_id":"t-001"}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "recorded")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddHistoryTrackNotFound(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM play_history WHERE user_id=$1 AND track_id=$2`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT library_id FROM tracks WHERE id = $1`)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.AddHistory(rec, udRequest(http.MethodPost, "/api/user/history",
		`{"track_id":"missing"}`, "u-001"))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "track not found")
}

func TestRemoveHistoryItems(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM play_history WHERE id = ANY($1) AND user_id = $2`)).
		WithArgs(sqlmock.AnyArg(), "u-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec := httptest.NewRecorder()
	h.RemoveHistoryItems(rec, udRequest(http.MethodPost, "/api/user/history/remove",
		`{"ids":["h-1"]}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRemoveHistoryItemsEmpty(t *testing.T) {
	h, _ := newUserDataHandler(t)

	rec := httptest.NewRecorder()
	h.RemoveHistoryItems(rec, udRequest(http.MethodPost, "/api/user/history/remove", `{"ids":[]}`, "u-001"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---- Playlists ----

func TestListPlaylists(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, name, track_ids, is_public, created_at FROM playlists WHERE owner_id = $1 ORDER BY name`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "track_ids", "is_public", "created_at"}).
			AddRow("pl-1", "Mix", []byte(`["t-1"]`), true, time.Now()))

	rec := httptest.NewRecorder()
	h.ListPlaylists(rec, udRequest(http.MethodGet, "/api/user/playlists", "", "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"Mix"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreatePlaylist(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO playlists`)).
		WithArgs(sqlmock.AnyArg(), "Road Trip", "u-001", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	rec := httptest.NewRecorder()
	h.CreatePlaylist(rec, udRequest(http.MethodPost, "/api/user/playlists",
		`{"name":"Road Trip"}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"created"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetPlaylistEmptyTracks(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT name, track_ids, created_at FROM playlists WHERE id = $1 AND owner_id = $2`)).
		WithArgs("pl-1", "u-001").
		WillReturnRows(sqlmock.NewRows([]string{"name", "track_ids", "created_at"}).
			AddRow("Mix", []byte("[]"), time.Now()))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/user/playlists/pl-1", nil),
		map[string]string{"id": "pl-1"})
	rec := httptest.NewRecorder()
	h.GetPlaylist(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"Mix"`)
	assert.Contains(t, rec.Body.String(), `"tracks":null`, "empty playlist yields nil tracks")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetPlaylistNotFound(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT name, track_ids, created_at FROM playlists WHERE id = $1 AND owner_id = $2`)).
		WithArgs("nope", "u-001").
		WillReturnError(sql.ErrNoRows)

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/user/playlists/nope", nil),
		map[string]string{"id": "nope"})
	rec := httptest.NewRecorder()
	h.GetPlaylist(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetPlaylistZeroPerPage(t *testing.T) {
	h, _ := newUserDataHandler(t)

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/user/playlists/pl-1?per_page=0", nil),
		map[string]string{"id": "pl-1"})
	rec := httptest.NewRecorder()
	h.GetPlaylist(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"tracks":[]`)
}

func TestDeletePlaylist(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM playlists WHERE id = $1 AND owner_id = $2`)).
		WithArgs("pl-1", "u-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodDelete, "/api/user/playlists/pl-1", nil),
		map[string]string{"id": "pl-1"})
	rec := httptest.NewRecorder()
	h.DeletePlaylist(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddTrackToPlaylistAlreadyExists(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT track_ids FROM playlists WHERE id = $1 AND owner_id = $2`)).
		WithArgs("pl-1", "u-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_ids"}).AddRow([]byte(`["t-001"]`)))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/user/playlists/pl-1/tracks",
		strings.NewReader(`{"track_id":"t-001"}`)), map[string]string{"id": "pl-1"})
	rec := httptest.NewRecorder()
	h.AddTrackToPlaylist(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "already exists")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddTrackToPlaylistSuccess(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT track_ids FROM playlists WHERE id = $1 AND owner_id = $2`)).
		WithArgs("pl-1", "u-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_ids"}).AddRow([]byte(`["t-001"]`)))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE playlists SET track_ids = $1, updated_at = NOW() WHERE id = $2 AND owner_id = $3`)).
		WithArgs([]byte(`["t-001","t-002"]`), "pl-1", "u-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/user/playlists/pl-1/tracks",
		strings.NewReader(`{"track_id":"t-002"}`)), map[string]string{"id": "pl-1"})
	rec := httptest.NewRecorder()
	h.AddTrackToPlaylist(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "added")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAddTracksToPlaylistDeduplicates(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT track_ids FROM playlists WHERE id = $1 AND owner_id = $2`)).
		WithArgs("pl-1", "u-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_ids"}).AddRow([]byte(`["t-001"]`)))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE playlists SET track_ids = $1, updated_at = NOW() WHERE id = $2 AND owner_id = $3`)).
		WithArgs([]byte(`["t-001","t-002","t-003"]`), "pl-1", "u-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/user/playlists/pl-1/tracks/batch",
		strings.NewReader(`{"track_ids":["t-001","t-002","t-003","t-002"]}`)), map[string]string{"id": "pl-1"})
	rec := httptest.NewRecorder()
	h.AddTracksToPlaylist(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRemoveTracksFromPlaylist(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT track_ids FROM playlists WHERE id = $1 AND owner_id = $2`)).
		WithArgs("pl-1", "u-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_ids"}).AddRow([]byte(`["t-001","t-002"]`)))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE playlists SET track_ids = $1, updated_at = NOW() WHERE id = $2 AND owner_id = $3`)).
		WithArgs([]byte(`["t-002"]`), "pl-1", "u-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := mux.SetURLVars(httptest.NewRequest(http.MethodPost, "/api/user/playlists/pl-1/tracks/remove",
		strings.NewReader(`{"track_ids":["t-001"]}`)), map[string]string{"id": "pl-1"})
	rec := httptest.NewRecorder()
	h.RemoveTracksFromPlaylist(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "removed")
	require.NoError(t, mock.ExpectationsWereMet())
}

// ---- Settings ----

func TestUserDataGetSettings(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT key, value FROM user_settings WHERE user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
			AddRow("theme", "dark").
			AddRow("lang", "zh"))

	rec := httptest.NewRecorder()
	h.GetSettings(rec, udRequest(http.MethodGet, "/api/user/settings", "", "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"theme":"dark"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserDataUpdateSettings(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO user_settings (user_id, key, value) VALUES ($1, $2, $3) ON CONFLICT (user_id, key) DO UPDATE SET value = $3`)).
		WithArgs("u-001", "theme", "light").
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec := httptest.NewRecorder()
	h.UpdateSettings(rec, udRequest(http.MethodPut, "/api/user/settings", `{"theme":"light"}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "saved")
	require.NoError(t, mock.ExpectationsWereMet())
}

// ---- Queue ----

func TestSaveQueue(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO user_settings (user_id, key, value) VALUES ($1, $2, $3) ON CONFLICT (user_id, key) DO UPDATE SET value = $3`)).
		WithArgs("u-001", "player_queue", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec := httptest.NewRecorder()
	h.SaveQueue(rec, udRequest(http.MethodPost, "/api/user/queue",
		`{"track_ids":["t-001"],"queue_idx":0,"mode":"normal"}`, "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetQueueEmpty(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM user_settings WHERE user_id = $1 AND key = 'player_queue'`)).
		WithArgs("u-001").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.GetQueue(rec, udRequest(http.MethodGet, "/api/user/queue", "", "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"queue":null`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetQueueWithTracks(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM user_settings WHERE user_id = $1 AND key = 'player_queue'`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"value"}).
			AddRow(`{"track_ids":["t-001"],"queue_idx":0,"shuffle_order":[],"shuffle_idx":0,"mode":"normal"}`))
	mock.ExpectQuery(regexp.QuoteMeta(`ORDER BY array_position($1::text[], t.id)`)).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "artist", "album_title", "album_id", "duration", "file_format", "cover_image_id", "track_number", "disc_number", "version", "version_label", "external_id", "metadata_source"}).
			AddRow("t-001", "Song", "Band", "Album", "alb-1", 200.0, "mp3", nil, 1, 1, 1, "", "", "musicbrainz"))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "external_id", "metadata_source"}))

	rec := httptest.NewRecorder()
	h.GetQueue(rec, udRequest(http.MethodGet, "/api/user/queue", "", "u-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"title":"Song"`)
	assert.Contains(t, rec.Body.String(), `"queue_idx":0`)
	require.NoError(t, mock.ExpectationsWereMet())
}

// ---- expandTrackVersions (white-box) ----

func TestExpandTrackVersionsNonTrack(t *testing.T) {
	h, _ := newUserDataHandler(t)

	ids := h.expandTrackVersions(t.Context(), "u-001", "album", []string{"alb-1", "alb-1"})
	assert.Equal(t, []string{"alb-1", "alb-1"}, ids, "non-track passes through untouched")
}

func TestExpandTrackVersionsNoLibraries(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE lm.user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}))

	ids := h.expandTrackVersions(t.Context(), "u-001", "track", []string{"t-001"})
	assert.Equal(t, []string{"t-001"}, ids)
}

func TestExpandTrackVersionsNoMBID(t *testing.T) {
	h, mock := newUserDataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE lm.user_id = $1`)).
		WithArgs("u-001").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "path", "owner_id", "metadata_storage_mode", "scan_interval",
			"last_scanned_at", "last_scan_errors", "track_count", "duration", "created_at", "updated_at"}).
			AddRow("lib-001", "L", "/m", "u-001", "database", "", nil, 0, 0, 0.0, time.Now(), time.Now()))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT external_id, metadata_source FROM tracks WHERE id = $1 AND external_id != '' AND library_id = ANY($2)`)).
		WithArgs("t-001", sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)

	ids := h.expandTrackVersions(t.Context(), "u-001", "track", []string{"t-001"})
	assert.Equal(t, []string{"t-001"}, ids)
}
