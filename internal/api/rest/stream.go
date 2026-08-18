package rest

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/internal/infrastructure/transcoder"
)

type StreamHandler struct {
	trackRepo    *repository.TrackRepo
	sessionStore *cache.SessionStore
	perm         *middleware.PermissionChecker
}

func NewStreamHandler(db *sql.DB, sessionStore *cache.SessionStore) *StreamHandler {
	return &StreamHandler{
		trackRepo:    repository.NewTrackRepo(db),
		sessionStore: sessionStore,
		perm:         middleware.NewPermissionChecker(db),
	}
}

func (h *StreamHandler) ServeStream(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	session := vars["session"]
	trackID := vars["id"]

	if session == "" {
		http.Error(w, "missing session", http.StatusUnauthorized)
		return
	}
	userID, err := h.sessionStore.Validate(r.Context(), session)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), trackID)
	if err != nil {
		http.Error(w, "track not found", http.StatusNotFound)
		return
	}

	if !h.perm.IsMember(r.Context(), track.LibraryID, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	quality := transcoder.ParseQuality(r.URL.Query().Get("quality"))
	if r.URL.Query().Get("init") == "1" || r.URL.Query().Get("start") != "" {
		transcoder.ServeTranscoded(r.Context(), w, r, track.FilePath, quality)
		return
	}
	if transcoder.Decide(track.BitRate, track.AudioCodec, quality).Transcode {
		transcoder.ServeTranscoded(r.Context(), w, r, track.FilePath, quality)
		return
	}

	w.Header().Set("Content-Type", "audio/"+track.FileFormat)
	w.Header().Set("Content-Length", strconv.FormatInt(track.FileSize, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeFile(w, r, track.FilePath)
}

// ServeTranscodeStatus reports whether the transcode cache for a track is ready,
// so the frontend can switch from the live stream to the seekable cache file.
func (h *StreamHandler) ServeTranscodeStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	session := vars["session"]
	trackID := vars["id"]

	if session == "" {
		http.Error(w, "missing session", http.StatusUnauthorized)
		return
	}
	userID, err := h.sessionStore.Validate(r.Context(), session)
	if err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), trackID)
	if err != nil {
		http.Error(w, "track not found", http.StatusNotFound)
		return
	}

	if !h.perm.IsMember(r.Context(), track.LibraryID, userID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	quality := transcoder.ParseQuality(r.URL.Query().Get("quality"))
	ready := transcoder.CacheReady(track.FilePath, quality)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ready":%t}`, ready)
}
