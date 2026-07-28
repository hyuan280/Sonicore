package rest

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type MetadataHandler struct {
	db         *sql.DB
	trackRepo  *repository.TrackRepo
	albumRepo  *repository.AlbumRepo
	artistRepo *repository.ArtistRepo
	mbCfg      metadata.MBConfig
}

func NewMetadataHandler(db *sql.DB, mbCfg metadata.MBConfig) *MetadataHandler {
	return &MetadataHandler{
		db:         db,
		trackRepo:  repository.NewTrackRepo(db),
		albumRepo:  repository.NewAlbumRepo(db),
		artistRepo: repository.NewArtistRepo(db),
		mbCfg:     mbCfg,
	}
}

func (h *MetadataHandler) Identify(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		TrackID string `json:"track_id"`
		MBID    string `json:"mbid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TrackID == "" || req.MBID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need track_id and mbid"})
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), req.TrackID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"})
		return
	}

	resolver := metadata.NewResolver(h.mbCfg)
	result, err := resolver.IdentifyTrack(r.Context(), req.MBID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resolver.Close()

	// Update track
	track.MBID = result.TrackMBID
	if result.Title != "" {
		track.Title = result.Title
	}
	if err := h.trackRepo.Update(r.Context(), track); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update track"})
		return
	}

	// Update artist if MBID found
	if result.ArtistMBID != "" {
		if artist, err := h.artistRepo.FindByID(r.Context(), track.ArtistID); err == nil && artist.MBID == "" {
			artist.MBID = result.ArtistMBID
			h.artistRepo.Update(r.Context(), artist)
		}
	}

	// Update album if MBID found
	if result.AlbumMBID != "" {
		if album, err := h.albumRepo.FindByID(r.Context(), track.AlbumID); err == nil && album.MBID == "" {
			album.MBID = result.AlbumMBID
			if result.Year != 0 {
				album.Year = result.Year
			}
			if result.Genre != "" {
				album.Genre = result.Genre
			}
			h.albumRepo.Update(r.Context(), album)
		}
	}

	// Return updated track info
	resp := map[string]interface{}{
		"track_id": track.ID,
		"mbid":     result.TrackMBID,
		"title":    result.Title,
	}
	if result.Artist != "" {
		resp["artist"] = result.Artist
	}
	if result.Album != "" {
		resp["album"] = result.Album
	}
	if result.Genre != "" {
		resp["genre"] = result.Genre
	}
	if result.Year != 0 {
		resp["year"] = result.Year
	}

	writeJSON(w, http.StatusOK, resp)
}
