package rest

import (
	"database/sql"
	"net/http"

	"github.com/sonicore/server/internal/infrastructure/lyrics"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type LyricsHandler struct {
	db          *sql.DB
	lyricsStore *lyrics.Store
	trackRepo   *repository.TrackRepo
}

func NewLyricsHandler(db *sql.DB, lyricsStore *lyrics.Store) *LyricsHandler {
	return &LyricsHandler{
		db:          db,
		lyricsStore: lyricsStore,
		trackRepo:   repository.NewTrackRepo(db),
	}
}

func (h *LyricsHandler) GetLyrics(w http.ResponseWriter, r *http.Request) {
	trackID := r.URL.Query().Get("trackid")
	if trackID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing trackid parameter"})
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), trackID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"})
		return
	}

	lyricsText, _, format, actualMask, err := h.lyricsStore.Get(track.LibraryID, trackID, track.LyricsMask)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"lyrics":      "",
			"format":      "",
			"lyrics_mask": 0,
		})
		return
	}

	// Repair stale mask if files were deleted
	if actualMask != track.LyricsMask {
		track.LyricsMask = actualMask
		h.trackRepo.Update(r.Context(), track)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"lyrics":      lyricsText,
		"format":      format,
		"lyrics_mask": actualMask,
	})
}
