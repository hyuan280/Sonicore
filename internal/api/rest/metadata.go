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
	umRepo     *repository.UserMetadataRepo
	mbCfg      metadata.MBConfig
}

func NewMetadataHandler(db *sql.DB, mbCfg metadata.MBConfig) *MetadataHandler {
	return &MetadataHandler{
		db:         db,
		trackRepo:  repository.NewTrackRepo(db),
		albumRepo:  repository.NewAlbumRepo(db),
		artistRepo: repository.NewArtistRepo(db),
		umRepo:     repository.NewUserMetadataRepo(db),
		mbCfg:     mbCfg,
	}
}

func (h *MetadataHandler) Save(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var req struct {
		TrackID     string `json:"track_id"`
		FileHash    string `json:"file_hash"`
		TrackMBID   string `json:"track_mbid"`
		Title       string `json:"title"`
		Artist      string `json:"artist"`
		Album       string `json:"album"`
		AlbumArtist string `json:"album_artist"`
		TrackNumber int    `json:"track_number"`
		DiscNumber  int    `json:"disc_number"`
		Year        int    `json:"year"`
		Genre       string `json:"genre"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileHash == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file_hash required"})
		return
	}

	err := h.umRepo.Upsert(r.Context(), &repository.UserMetadata{
		UserID:      userID,
		FileHash:    req.FileHash,
		TrackMBID:   req.TrackMBID,
		Title:       req.Title,
		Artist:      req.Artist,
		Album:       req.Album,
		AlbumArtist: req.AlbumArtist,
		TrackNumber: req.TrackNumber,
		DiscNumber:  req.DiscNumber,
		Year:        req.Year,
		Genre:       req.Genre,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Immediately update all metadata in DB
	if req.TrackID != "" {
		if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
			if req.Title != "" { track.Title = req.Title }
			if req.TrackMBID != "" { track.MBID = req.TrackMBID }
			h.trackRepo.Update(r.Context(), track)

			if req.Artist != "" && track.ArtistID != "" {
				if artist, err := h.artistRepo.FindByID(r.Context(), track.ArtistID); err == nil {
					artist.Name = req.Artist
					artist.SortName = req.Artist
					h.artistRepo.Update(r.Context(), artist)
				}
			}

			if req.Album != "" && track.AlbumID != "" {
				if album, err := h.albumRepo.FindByID(r.Context(), track.AlbumID); err == nil {
					album.Title = req.Album
					if req.Year != 0 { album.Year = req.Year }
					if req.Genre != "" { album.Genre = req.Genre }
					h.albumRepo.Update(r.Context(), album)
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (h *MetadataHandler) Search(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var req struct {
		TrackID string `json:"track_id"`
		Title   string `json:"title"`
		Artist  string `json:"artist"`
		Album   string `json:"album"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	// Check user_metadata cache first
	var fileHash string
	var cached *repository.UserMetadata
	if req.TrackID != "" {
		if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
			fileHash = track.Hash
			if um, err := h.umRepo.FindByUserAndHash(r.Context(), userID, track.Hash); err == nil {
				cached = um
			}
		}
	}

	// If cached data has all key fields, return it directly
	if cached != nil && cached.Title != "" && cached.Artist != "" && cached.Album != "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"matched":      true,
			"cached":       true,
			"file_hash":    fileHash,
			"title":        cached.Title,
			"artist":       cached.Artist,
			"album":        cached.Album,
			"year":         cached.Year,
			"genre":        cached.Genre,
			"track_mbid":   cached.TrackMBID,
		})
		return
	}

	// Fill search params from cached data if available
	if cached != nil {
		if req.Title == "" { req.Title = cached.Title }
		if req.Artist == "" { req.Artist = cached.Artist }
		if req.Album == "" { req.Album = cached.Album }
	}

	if req.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title required"})
		return
	}

	resolver := metadata.NewResolver(h.mbCfg)
	result, err := resolver.Enrich(r.Context(), &metadata.AudioMeta{
		Title:  req.Title,
		Artist: req.Artist,
		Album:  req.Album,
	})
	resolver.Close()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if result == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"matched": false})
		return
	}

	// Merge: cached data has priority, MB fills gaps
	t := result.Title
	if t == "" { t = req.Title }
	if cached != nil && cached.Title != "" { t = cached.Title }
	a := result.Artist
	if a == "" { a = req.Artist }
	if cached != nil && cached.Artist != "" { a = cached.Artist }
	al := result.Album
	if al == "" { al = req.Album }
	if cached != nil && cached.Album != "" { al = cached.Album }
	yr := result.Year
	if cached != nil && cached.Year != 0 { yr = cached.Year }
	if yr == 0 && req.TrackID != "" {
		if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
			if album, err := h.albumRepo.FindByID(r.Context(), track.AlbumID); err == nil {
				yr = album.Year
			}
		}
	}
	gen := result.Genre
	if cached != nil && cached.Genre != "" { gen = cached.Genre }
	tmbid := result.TrackMBID
	if cached != nil && cached.TrackMBID != "" { tmbid = cached.TrackMBID }

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"matched":      true,
		"file_hash":    fileHash,
		"title":        t,
		"artist":       a,
		"album":        al,
		"year":         yr,
		"genre":        gen,
		"track_mbid":   tmbid,
		"artist_mbid":  result.ArtistMBID,
		"album_mbid":   result.AlbumMBID,
	})
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

	track.MBID = result.TrackMBID
	if result.Title != "" {
		track.Title = result.Title
	}
	if err := h.trackRepo.Update(r.Context(), track); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update track"})
		return
	}

	if result.ArtistMBID != "" {
		if artist, err := h.artistRepo.FindByID(r.Context(), track.ArtistID); err == nil && artist.MBID == "" {
			artist.MBID = result.ArtistMBID
			h.artistRepo.Update(r.Context(), artist)
		}
	}

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
