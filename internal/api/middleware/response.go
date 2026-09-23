package middleware

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// StatusRecorder wraps a ResponseWriter to capture the response status code.
// It preserves the optional interfaces http.ServeFile and http.ResponseController
// rely on: ReadFrom keeps the sendfile fast path and Unwrap exposes the
// underlying writer.
type StatusRecorder struct {
	http.ResponseWriter
	status int
}

// NewStatusRecorder wraps w.
func NewStatusRecorder(w http.ResponseWriter) *StatusRecorder {
	return &StatusRecorder{ResponseWriter: w}
}

// Status returns the status code written so far, or 0 when none was written.
func (r *StatusRecorder) Status() int {
	return r.status
}

func (r *StatusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *StatusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (r *StatusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// ReadFrom preserves the sendfile fast path used by http.ServeFile.
func (r *StatusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if rf, ok := r.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	return io.Copy(r.ResponseWriter, src)
}

// audioContentTypes maps the common audio file extensions to their canonical
// MIME types. Extensions not listed (including non-audio ones) fall back to
// application/octet-stream rather than an unregistered "audio/<ext>".
var audioContentTypes = map[string]string{
	"mp3":  "audio/mpeg",
	"m4a":  "audio/mp4",
	"m4b":  "audio/mp4",
	"mp4":  "audio/mp4",
	"alac": "audio/mp4",
	"aac":  "audio/aac",
	"flac": "audio/flac",
	"ogg":  "audio/ogg",
	"oga":  "audio/ogg",
	"opus": "audio/opus",
	"wav":  "audio/wav",
	"aiff": "audio/aiff",
	"aif":  "audio/aiff",
	"wma":  "audio/x-ms-wma",
}

// AudioContentType derives an audio MIME type from a file path's extension.
// The stored track.FileFormat is ffprobe's format_name (a comma-separated list
// such as "mov,mp4,m4a,3gp,3g2,mj2"), so it must not be used as a MIME type or
// as a download extension.
func AudioContentType(filePath string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	if ct, ok := audioContentTypes[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}
