package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var DBClosedErr = errors.New("database is closed")

func TestHealthUp(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	handler := NewHealthHandler(db)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
	assert.Equal(t, "up", body["database"])
	assert.Equal(t, "0.1.0", body["version"])
}

func TestHealthDatabaseDown(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectPing().WillReturnError(DBClosedErr)

	handler := NewHealthHandler(db)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "degraded", body["status"])
	assert.Equal(t, "down", body["database"])
	require.NoError(t, mock.ExpectationsWereMet())
}
