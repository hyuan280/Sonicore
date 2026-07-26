package rest

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type StreamHandler struct {
	db           *sql.DB
	trackRepo    *repository.TrackRepo
	sessionStore *cache.SessionStore
}

func NewStreamHandler(db *sql.DB, sessionStore *cache.SessionStore) *StreamHandler {
	return &StreamHandler{
		db:           db,
		trackRepo:    repository.NewTrackRepo(db),
		sessionStore: sessionStore,
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
	if _, err := h.sessionStore.Validate(r.Context(), session); err != nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), trackID)
	if err != nil {
		http.Error(w, "track not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "audio/"+track.FileFormat)
	w.Header().Set("Content-Length", strconv.FormatInt(track.FileSize, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeFile(w, r, track.FilePath)
}
