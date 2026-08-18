package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/domain"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   interface{}
	}{
		{"ok with object", http.StatusOK, map[string]string{"message": "hi"}},
		{"created with list", http.StatusCreated, []int{1, 2, 3}},
		{"no content null", http.StatusNoContent, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeJSON(rec, tt.status, tt.body)

			assert.Equal(t, tt.status, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			if tt.body != nil {
				var decoded interface{}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &decoded))
				assert.NotNil(t, decoded)
			}
		})
	}
}

func TestWriteCodedError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeCodedError(rec, http.StatusUnauthorized, domain.ErrUnauthorized)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "Unauthorized", body["error"])
	assert.Equal(t, float64(1), body["code"])
}

func TestWriteCodedErrorVariousCodes(t *testing.T) {
	tests := []struct {
		status int
		code   domain.ErrorCode
		msg    string
	}{
		{http.StatusBadRequest, domain.ErrInvalidBody, "Invalid request body"},
		{http.StatusNotFound, domain.ErrNotFound, "Not found"},
		{http.StatusForbidden, domain.ErrLibAccessDenied, "Access denied"},
	}

	for _, tt := range tests {
		t.Run(tt.code.Key(), func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeCodedError(rec, tt.status, tt.code)

			var body map[string]interface{}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tt.msg, body["error"])
			assert.Equal(t, float64(tt.code), body["code"])
		})
	}
}
