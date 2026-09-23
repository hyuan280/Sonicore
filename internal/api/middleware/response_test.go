package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAudioContentType(t *testing.T) {
	cases := map[string]string{
		"/m/song.mp3":  "audio/mpeg",
		"/m/song.MP3":  "audio/mpeg",
		"/m/song.m4a":  "audio/mp4",
		"/m/song.flac": "audio/flac",
		"/m/song.ogg":  "audio/ogg",
		"/m/song.wav":  "audio/wav",
		"/m/song.txt":  "application/octet-stream",
		"/m/song":      "application/octet-stream",
	}
	for path, want := range cases {
		assert.Equal(t, want, AudioContentType(path), path)
	}
}

func TestStatusRecorderCapturesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := NewStatusRecorder(rec)

	sr.WriteHeader(http.StatusCreated)
	_, err := sr.Write([]byte("hi"))
	require.NoError(t, err)

	assert.Equal(t, http.StatusCreated, sr.Status())
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "hi", rec.Body.String())
	assert.Same(t, rec, sr.Unwrap())
}

func TestStatusRecorderReadFromUsesUnderlyingReaderFrom(t *testing.T) {
	rw := &readerFromRecorder{ResponseWriter: httptest.NewRecorder()}
	sr := NewStatusRecorder(rw)

	n, err := sr.ReadFrom(bytes.NewReader([]byte("payload")))
	require.NoError(t, err)

	assert.Equal(t, int64(len("payload")), n)
	assert.True(t, rw.used, "underlying io.ReaderFrom must be used")
	assert.Equal(t, http.StatusOK, sr.Status())
}

type readerFromRecorder struct {
	http.ResponseWriter
	used bool
}

func (r *readerFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.used = true
	return io.Copy(r.ResponseWriter, src)
}
