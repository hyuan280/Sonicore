package rest

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMetadataHandler(t *testing.T) (*MetadataHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewMetadataHandler(db, metadata.MBConfig{RateLimit: 10000}), mock
}

// expectMBSettings mocks the two settings reads done by mbConfig().
func expectMBSettings(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM server_settings WHERE key=$1`)).
		WithArgs("metadata_musicbrainz_api_url").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT value FROM server_settings WHERE key=$1`)).
		WithArgs("metadata_musicbrainz_rate_limit").
		WillReturnError(sql.ErrNoRows)
}

func metadataTrackRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "library_id", "title", "cover_image_id",
		"duration", "bit_rate", "sample_rate", "channels",
		"file_path", "file_size", "file_format", "audio_codec", "mbid", "acoust_id", "hash",
		"lyrics_mask", "lyrics_offset", "heat", "play_count", "last_played_at", "metadata", "version", "version_label", "created_at", "updated_at"}).
		AddRow("t-001", "lib-001", "Song", nil, 200, 320, 44100, 2, "/m/song.flac", 1000, "flac", "flac", "", "", "h",
			0, 0, 0, 0, nil, nil, 1, "", time.Now(), time.Now())
}

// newMBTestServer starts a fake MusicBrainz API server for metadata handlers.
func newMBTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestMetadataIdentifyUnauthorized(t *testing.T) {
	h, _ := newMetadataHandler(t)

	rec := httptest.NewRecorder()
	h.Identify(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/identify",
		strings.NewReader(`{"track_id":"t-001","mbid":"mbid-1"}`)))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMetadataIdentifyMissingParams(t *testing.T) {
	h, _ := newMetadataHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/metadata/identify", strings.NewReader(`{"track_id":"t-001"}`))
	rec := httptest.NewRecorder()
	h.Identify(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "need track_id and mbid")
}

func TestMetadataIdentifyTrackNotFound(t *testing.T) {
	h, mock := newMetadataHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodPost, "/api/metadata/identify",
		strings.NewReader(`{"track_id":"missing","mbid":"mbid-1"}`))
	rec := httptest.NewRecorder()
	h.Identify(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMetadataIdentifySuccess(t *testing.T) {
	h, mock := newMetadataHandler(t)
	srv := newMBTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/recording/mbid-9":
			fmt.Fprint(w, `{"id":"mbid-9","title":"Song (Live)",
				"artist-credit":[{"name":"Band","artist":{"id":"a-9","country":"FR"}}],
				"releases":[{"id":"rel-9","title":"Album","date":"2005-03-01","status":"Official"}]}`)
		case "/release/rel-9":
			fmt.Fprint(w, `{"id":"rel-9","tags":[{"name":"pop","count":4}]}`)
		case "/artist/a-9":
			fmt.Fprint(w, `{"id":"a-9","tags":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	h.mbCfg.APIURL = srv.URL

	// trackRepo.FindByID runs BEFORE the resolver is built
	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnRows(metadataTrackRows())
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_albums ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "album_id", "track_number", "disc_number", "title", "cover_image_id"}).
			AddRow("t-001", "alb-1", 1, 1, "Album", nil))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "mbid"}).
			AddRow("t-001", "art-1", "performer", 0, "Band", ""))

	// mbConfig settings reads (when the resolver is created)
	expectMBSettings(mock)

	// trackRepo.Update (transaction) — replaces track_albums too
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE tracks SET title=$1, cover_image_id=$2,
		 duration=$3, bit_rate=$4, sample_rate=$5, channels=$6,
		 file_path=$7, file_size=$8, file_format=$9, audio_codec=$10, mbid=$11, acoust_id=$12,
		 hash=$13, lyrics_mask=$14, lyrics_offset=$15, heat=$16, play_count=$17,
		 last_played_at=$18, metadata=$19, version=$20, version_label=$21, updated_at=NOW()
		 WHERE id=$22`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM track_albums WHERE track_id = $1`)).
		WithArgs("t-001").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectPrepare(regexp.QuoteMeta(`INSERT INTO track_albums (track_id, album_id, track_number, disc_number)`))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO track_albums`)).
		WithArgs("t-001", "alb-1", 1, 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// LoadTrackArtists for artist mbid update
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "mbid"}).
			AddRow("t-001", "art-1", "performer", 0, "Band", ""))
	// artistRepo.FindByID
	mock.ExpectQuery(regexp.QuoteMeta(`FROM artists WHERE id = $1`)).
		WithArgs("art-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "sort_name", "mbid", "country", "biography", "cover_image_id", "track_count", "created_at", "updated_at", "roles"}).
			AddRow("art-1", "Band", "Band", "", "", "", nil, 0, time.Now(), time.Now(), ""))
	// artistRepo.Update
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE artists SET name=$1, sort_name=$2, mbid=$3, country=$4, biography=$5,
		 cover_image_id=$6, updated_at=NOW()
		 WHERE id=$7`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// albumRepo.FindByID
	mock.ExpectQuery(regexp.QuoteMeta(`FROM albums WHERE id = $1`)).
		WithArgs("alb-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "artist_id", "mbid", "country", "year", "genre", "cover_image_id", "song_count", "duration", "created_at", "updated_at"}).
			AddRow("alb-1", "Album", "art-1", "", "", 0, "", nil, 0, 0.0, time.Now(), time.Now()))
	// albumRepo.Update
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE albums SET title=$1, artist_id=$2, mbid=$3, country=$4, year=$5, genre=$6,
		 cover_image_id=$7, song_count=$8, duration=$9, updated_at=NOW()
		 WHERE id=$10`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPost, "/api/metadata/identify",
		strings.NewReader(`{"track_id":"t-001","mbid":"mbid-9"}`))
	rec := httptest.NewRecorder()
	h.Identify(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"mbid":"mbid-9"`)
	assert.Contains(t, rec.Body.String(), `"title":"Song"`, "paren suffix trimmed")
	assert.Contains(t, rec.Body.String(), `"artist":"Band"`)
	assert.Contains(t, rec.Body.String(), `"year":2005`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMetadataReidentifyUnauthorized(t *testing.T) {
	h, _ := newMetadataHandler(t)

	rec := httptest.NewRecorder()
	h.Reidentify(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/reidentify",
		strings.NewReader(`{"track_id":"t-001"}`)))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMetadataReidentifyMissingTrackID(t *testing.T) {
	h, _ := newMetadataHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/metadata/reidentify", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.Reidentify(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMetadataReidentifyProbeFails(t *testing.T) {
	h, mock := newMetadataHandler(t)

	// track find
	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnRows(metadataTrackRows())
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_albums ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "album_id", "track_number", "disc_number", "title", "cover_image_id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs("t-001").
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "mbid"}))

	// file does not exist → metadata.Probe fails
	req := httptest.NewRequest(http.MethodPost, "/api/metadata/reidentify",
		strings.NewReader(`{"track_id":"t-001"}`))
	rec := httptest.NewRecorder()
	h.Reidentify(rec, req.WithContext(contextWithUserID(req.Context(), "u-001")))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to probe file")
}

func TestMetadataSearchArtistMissingName(t *testing.T) {
	h, _ := newMetadataHandler(t)

	rec := httptest.NewRecorder()
	h.SearchArtist(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/search-artist", strings.NewReader(`{}`)))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "name required")
}

func TestMetadataSearchArtistSuccess(t *testing.T) {
	h, mock := newMetadataHandler(t)
	srv := newMBTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"artists":[{"id":"a-1","name":"Band","country":"GB","type":"Group"}]}`)
	})
	h.mbCfg.APIURL = srv.URL
	expectMBSettings(mock)

	rec := httptest.NewRecorder()
	h.SearchArtist(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/search-artist",
		strings.NewReader(`{"name":"Band"}`)))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"Band"`)
	assert.Contains(t, rec.Body.String(), `"mbid":"a-1"`)
}

func TestMetadataSearchReleaseMissingName(t *testing.T) {
	h, _ := newMetadataHandler(t)

	rec := httptest.NewRecorder()
	h.SearchRelease(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/search-release", strings.NewReader(`{}`)))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMetadataSearchReleaseSuccess(t *testing.T) {
	h, mock := newMetadataHandler(t)
	srv := newMBTestServer(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"releases":[{"id":"rel-1","title":"Album","status":"Official",
			"artist-credit":[{"name":"Band"}]}]}`)
	})
	h.mbCfg.APIURL = srv.URL
	expectMBSettings(mock)

	rec := httptest.NewRecorder()
	h.SearchRelease(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/search-release",
		strings.NewReader(`{"name":"Album"}`)))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"mbid":"rel-1"`)
	// query text prepended as unmatched entry (mbid empty) plus the matched one
	assert.Equal(t, 2, strings.Count(rec.Body.String(), `"title":"Album"`))
	assert.Contains(t, rec.Body.String(), `"mbid":""`)
}

func TestMetadataSearchTrackInvalidBody(t *testing.T) {
	h, _ := newMetadataHandler(t)

	rec := httptest.NewRecorder()
	h.SearchTrack(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/search-track",
		strings.NewReader("not-json")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestMetadataSearchTrackTitleRequired(t *testing.T) {
	h, mock := newMetadataHandler(t)

	// no track_id, no mbid → falls through to title check
	rec := httptest.NewRecorder()
	h.SearchTrack(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/search-track",
		strings.NewReader(`{}`)))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "title required")
	require.NoError(t, mock.ExpectationsWereMet())
}
