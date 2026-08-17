package rest

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/lyrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expectTrackFindByID mocks the three queries issued by TrackRepo.FindByID.
func expectTrackFindByID(mock sqlmock.Sqlmock, track *domain.Track) {
	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs(track.ID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "library_id", "title", "cover_image_id",
			"duration", "bit_rate", "sample_rate", "channels",
			"file_path", "file_size", "file_format", "audio_codec", "external_id", "metadata_source", "external_ids", "acoust_id", "hash",
			"lyrics_mask", "lyrics_offset", "heat", "play_count", "last_played_at", "metadata", "version", "version_label", "created_at", "updated_at"}).
			AddRow(track.ID, track.LibraryID, track.Title, track.CoverImageID,
				track.Duration, track.BitRate, track.SampleRate, track.Channels,
				track.FilePath, track.FileSize, track.FileFormat, track.AudioCodec, track.ExternalID, "musicbrainz", "{}", track.AcoustID, track.Hash,
				track.LyricsMask, track.LyricsOffset, track.Heat, track.PlayCount, track.LastPlayedAt, track.Metadata, track.Version, track.VersionLabel, track.CreatedAt, track.UpdatedAt))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_albums ta`)).
		WithArgs(track.ID).
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "album_id", "track_number", "disc_number", "title", "cover_image_id"}))
	mock.ExpectQuery(regexp.QuoteMeta(`FROM track_artists ta`)).
		WithArgs(track.ID).
		WillReturnRows(sqlmock.NewRows([]string{"track_id", "artist_id", "role", "sort_order", "name", "external_id", "metadata_source"}))
}

func newLyricsHandler(t *testing.T) (*LyricsHandler, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewLyricsHandler(db, lyrics.NewStore(t.TempDir())), mock
}

func lyricsTestTrack() *domain.Track {
	now := time.Now()
	return &domain.Track{
		ID:          "t-001",
		LibraryID:   "lib-001",
		Title:       "Song",
		Duration:    200,
		FilePath:    "/m/song.mp3",
		FileSize:    1000,
		FileFormat:  "mp3",
		AudioCodec:  "mp3",
		Hash:        "h",
		LyricsMask:  lyrics.PriorityBit(lyrics.PriorityUser),
		LyricsOffset: 1.5,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func lyricsGetRequest(trackID string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/lyrics?trackid="+trackID, nil)
}

func TestLyricsGetMissingTrackID(t *testing.T) {
	h, _ := newLyricsHandler(t)

	rec := httptest.NewRecorder()
	h.GetLyrics(rec, httptest.NewRequest(http.MethodGet, "/api/lyrics", nil))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing trackid")
}

func TestLyricsGetTrackNotFound(t *testing.T) {
	h, mock := newLyricsHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.GetLyrics(rec, lyricsGetRequest("missing"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "track not found")
}

func TestLyricsGetFound(t *testing.T) {
	h, mock := newLyricsHandler(t)
	track := lyricsTestTrack()
	expectTrackFindByID(mock, track)

	require.NoError(t, h.lyricsStore.Save(track.LibraryID, track.ID, lyrics.PriorityUser, "[00:01.00]line"))

	rec := httptest.NewRecorder()
	h.GetLyrics(rec, lyricsGetRequest(track.ID))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "[00:01.00]line", body["lyrics"])
	assert.Equal(t, "lrc", body["format"])
	assert.Equal(t, float64(lyrics.PriorityBit(lyrics.PriorityUser)), body["lyrics_mask"])
	assert.Equal(t, 1.5, body["lyrics_offset"])
}

func TestLyricsGetNoLyricsReturnsEmpty(t *testing.T) {
	h, mock := newLyricsHandler(t)
	track := lyricsTestTrack()
	expectTrackFindByID(mock, track)

	rec := httptest.NewRecorder()
	h.GetLyrics(rec, lyricsGetRequest(track.ID))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "", body["lyrics"])
	assert.Equal(t, "", body["format"])
	assert.Equal(t, float64(0), body["lyrics_mask"])
}

func TestLyricsGetRepairsStaleMask(t *testing.T) {
	h, mock := newLyricsHandler(t)
	track := lyricsTestTrack()
	// mask claims user + network, but only the user file exists
	track.LyricsMask = lyrics.PriorityBit(lyrics.PriorityUser) | lyrics.PriorityBit(lyrics.PriorityNetwork)
	expectTrackFindByID(mock, track)

	require.NoError(t, h.lyricsStore.Save(track.LibraryID, track.ID, lyrics.PriorityUser, "user lyrics"))

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE tracks SET title=$1, cover_image_id=$2,
		 duration=$3, bit_rate=$4, sample_rate=$5, channels=$6,
		 file_path=$7, file_size=$8, file_format=$9, audio_codec=$10, external_id=$11, metadata_source=$12, external_ids=$13, acoust_id=$14,
		 hash=$15, lyrics_mask=$16, lyrics_offset=$17, heat=$18, play_count=$19,
		 last_played_at=$20, metadata=$21, version=$22, version_label=$23, updated_at=NOW()
		 WHERE id=$24`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	rec := httptest.NewRecorder()
	h.GetLyrics(rec, lyricsGetRequest(track.ID))

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, float64(lyrics.PriorityBit(lyrics.PriorityUser)), body["lyrics_mask"],
		"missing network bit reported in actual mask")
	assert.Equal(t, "user lyrics", body["lyrics"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLyricsUpdateSuccess(t *testing.T) {
	h, mock := newLyricsHandler(t)
	track := lyricsTestTrack()
	expectTrackFindByID(mock, track)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE tracks SET lyrics_offset=$1, updated_at=NOW() WHERE id=$2`)).
		WithArgs(2.5, "t-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	req := httptest.NewRequest(http.MethodPost, "/api/lyrics",
		strings.NewReader(`{"trackid":"t-001","offset":2.5}`))
	rec := httptest.NewRecorder()
	h.UpdateLyrics(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"lyrics_offset":2.5`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLyricsUpdateInvalidBody(t *testing.T) {
	h, _ := newLyricsHandler(t)

	rec := httptest.NewRecorder()
	h.UpdateLyrics(rec, httptest.NewRequest(http.MethodPost, "/api/lyrics",
		strings.NewReader("not-json")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid request body")
}

func TestLyricsUpdateMissingTrackID(t *testing.T) {
	h, _ := newLyricsHandler(t)

	rec := httptest.NewRecorder()
	h.UpdateLyrics(rec, httptest.NewRequest(http.MethodPost, "/api/lyrics",
		strings.NewReader(`{"offset":1}`)))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing trackid")
}

func TestLyricsUpdateTrackNotFound(t *testing.T) {
	h, mock := newLyricsHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.UpdateLyrics(rec, httptest.NewRequest(http.MethodPost, "/api/lyrics",
		strings.NewReader(`{"trackid":"missing","offset":1}`)))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "track not found")
}

func TestLyricsUpdateLoadError(t *testing.T) {
	h, mock := newLyricsHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM tracks WHERE id = $1`)).
		WithArgs("t-001").
		WillReturnError(sql.ErrConnDone)

	rec := httptest.NewRecorder()
	h.UpdateLyrics(rec, httptest.NewRequest(http.MethodPost, "/api/lyrics",
		strings.NewReader(`{"trackid":"t-001","offset":1}`)))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to load track")
}

func TestLyricsUpdateOffsetError(t *testing.T) {
	h, mock := newLyricsHandler(t)
	track := lyricsTestTrack()
	expectTrackFindByID(mock, track)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE tracks SET lyrics_offset=$1, updated_at=NOW() WHERE id=$2`)).
		WillReturnError(sql.ErrConnDone)

	rec := httptest.NewRecorder()
	h.UpdateLyrics(rec, httptest.NewRequest(http.MethodPost, "/api/lyrics",
		strings.NewReader(`{"trackid":"t-001","offset":1}`)))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "failed to update lyrics offset")
}
