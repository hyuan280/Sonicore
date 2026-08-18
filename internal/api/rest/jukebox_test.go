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
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/api/ws"
	"github.com/sonicore/server/internal/infrastructure/player"
)

func newJukeboxHandler(t *testing.T) (*JukeboxHandler, sqlmock.Sqlmock) {
	t.Helper()
	t.Setenv("PULSE_SERVER", "")
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewJukeboxHandler(db, player.NewEngineManager(), ws.NewHub()), mock
}

func jukeboxRow(id, name string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "device_id", "device_name", "device_config_id", "device_driver", "volume", "play_mode", "queue", "queue_idx",
		"shuffle_order", "shuffle_idx", "path_mapping", "created_at", "updated_at"}).
		AddRow(id, name, "hw:1,0", "USB DAC", "", "alsa", 0.8, "normal", []byte("null"), 0, []byte("null"), 0, []byte("null"), time.Now(), time.Now())
}

func jbRequest(method, path, id, body string) *http.Request {
	req := mux.SetURLVars(httptest.NewRequest(method, path, strings.NewReader(body)), map[string]string{"id": id})
	return req
}

func TestJukeboxListEmpty(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM jukeboxes ORDER BY created_at`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "device_id", "device_name", "device_config_id", "device_driver", "volume", "play_mode", "queue", "queue_idx",
			"shuffle_order", "shuffle_idx", "path_mapping", "created_at", "updated_at"}))

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/jukeboxes", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"jukeboxes":[]`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJukeboxListError(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM jukeboxes ORDER BY created_at`)).
		WillReturnError(sql.ErrConnDone)

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/jukeboxes", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestJukeboxCreateInvalidBody(t *testing.T) {
	h, _ := newJukeboxHandler(t)

	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/jukeboxes", strings.NewReader("not-json")))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestJukeboxCreateMissingName(t *testing.T) {
	h, _ := newJukeboxHandler(t)

	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/jukeboxes", strings.NewReader(`{}`)))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "name is required")
}

func TestJukeboxCreateDeviceConfigNotFound(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM audio_devices WHERE id = $1`)).
		WithArgs("cfg-1").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/jukeboxes",
		strings.NewReader(`{"name":"J","device_config_id":"cfg-1"}`)))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "device config not found")
}

func TestJukeboxCreateDeviceAlreadyBound(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM audio_devices WHERE id = $1`)).
		WithArgs("cfg-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "device_type", "device_id", "driver", "config", "created_at", "updated_at"}).
			AddRow("cfg-1", "DAC", "local", "hw:1,0", "alsa", []byte("null"), time.Now(), time.Now()))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id FROM jukeboxes WHERE device_config_id = $1`)).
		WithArgs("cfg-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("jb-other"))

	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/jukeboxes",
		strings.NewReader(`{"name":"J","device_config_id":"cfg-1"}`)))

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "already bound")
}

func TestJukeboxCreateSuccessWithDefaults(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO jukeboxes`)).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(time.Now(), time.Now()))

	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/api/jukeboxes",
		strings.NewReader(`{"name":"Living Room"}`)))

	assert.Equal(t, http.StatusCreated, rec.Code)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Living Room", body["name"])
	assert.Equal(t, "default", body["device_id"], "no device → default")
	assert.Equal(t, 0.8, body["volume"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJukeboxGetNotFound(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM jukeboxes WHERE id = $1`)).
		WithArgs("nope").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.Get(rec, jbRequest(http.MethodGet, "/api/jukeboxes/nope", "nope", ""))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestJukeboxGetSuccess(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM jukeboxes WHERE id = $1`)).
		WithArgs("jb-1").
		WillReturnRows(jukeboxRow("jb-1", "Living Room"))

	rec := httptest.NewRecorder()
	h.Get(rec, jbRequest(http.MethodGet, "/api/jukeboxes/jb-1", "jb-1", ""))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"Living Room"`)
	assert.Contains(t, rec.Body.String(), `"volume":0.8`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJukeboxDelete(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM jukeboxes WHERE id = $1`)).
		WithArgs("jb-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec := httptest.NewRecorder()
	h.Delete(rec, jbRequest(http.MethodDelete, "/api/jukeboxes/jb-1", "jb-1", ""))

	assert.Equal(t, http.StatusNoContent, rec.Code)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJukeboxDeleteError(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM jukeboxes WHERE id = $1`)).
		WillReturnError(sql.ErrConnDone)

	rec := httptest.NewRecorder()
	h.Delete(rec, jbRequest(http.MethodDelete, "/api/jukeboxes/jb-1", "jb-1", ""))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestJukeboxUpdateNotFound(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM jukeboxes WHERE id = $1`)).
		WithArgs("nope").
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	h.Update(rec, jbRequest(http.MethodPut, "/api/jukeboxes/nope", "nope", `{"name":"X"}`))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestJukeboxUpdateSuccess(t *testing.T) {
	h, mock := newJukeboxHandler(t)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM jukeboxes WHERE id = $1`)).
		WithArgs("jb-1").
		WillReturnRows(jukeboxRow("jb-1", "Living Room"))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE jukeboxes SET name=$1, device_id=$2, device_name=$3, device_config_id=$4, device_driver=$5, volume=$6, play_mode=$7,
		       queue=$8, queue_idx=$9, shuffle_order=$10, shuffle_idx=$11,
		       path_mapping=$12, updated_at=$13
		WHERE id=$14`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	rec := httptest.NewRecorder()
	h.Update(rec, jbRequest(http.MethodPut, "/api/jukeboxes/jb-1", "jb-1", `{"name":"Renamed"}`))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"Renamed"`)
	require.NoError(t, mock.ExpectationsWereMet())
}
