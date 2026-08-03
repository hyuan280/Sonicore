package rest

import (
	"database/sql"
	"encoding/json"
	"errors"
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
		"lyrics":        lyricsText,
		"format":        format,
		"lyrics_mask":   actualMask,
		"lyrics_offset": track.LyricsOffset,
	})
}

type updateLyricsReq struct {
	TrackID string  `json:"trackid"`
	Offset  float64 `json:"offset"`
}

func (h *LyricsHandler) UpdateLyrics(w http.ResponseWriter, r *http.Request) {
	var req updateLyricsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.TrackID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing trackid"})
		return
	}

	_, err := h.trackRepo.FindByID(r.Context(), req.TrackID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load track"})
		}
		return
	}

	if err := h.trackRepo.UpdateLyricsOffset(r.Context(), req.TrackID, req.Offset); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update lyrics offset"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"lyrics_offset": req.Offset,
	})
}
