package rest

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sonicore/server/internal/core/domain"
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
		writeCodedError(w, http.StatusBadRequest, domain.ErrLyricsTrackIDRequired)
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), trackID)
	if err != nil {
		writeCodedError(w, http.StatusNotFound, domain.ErrMetaTrackNotFound)
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
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if req.TrackID == "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrLyricsTrackIDRequired)
		return
	}

	_, err := h.trackRepo.FindByID(r.Context(), req.TrackID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeCodedError(w, http.StatusNotFound, domain.ErrMetaTrackNotFound)
		} else {
			writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		}
		return
	}

	if err := h.trackRepo.UpdateLyricsOffset(r.Context(), req.TrackID, req.Offset); err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrLyricsUpdateOffsetFailed)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"lyrics_offset": req.Offset,
	})
}
