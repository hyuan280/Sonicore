package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
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
		TrackID   string          `json:"track_id"`
		FileHash  string          `json:"file_hash"`
		TrackMBID string          `json:"track_mbid"`
		Title     string          `json:"title"`
		Album     string          `json:"album"`
		Year      int             `json:"year"`
		Genre     string          `json:"genre"`
		Artists   []struct {
			Name string `json:"name"`
			MBID string `json:"mbid"`
		} `json:"artists"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileHash == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file_hash required"})
		return
	}

	// Build artist string from artists array
	artistStr := ""
	for i, a := range req.Artists {
		if i > 0 {
			artistStr += ","
		}
		artistStr += a.Name
	}

	err := h.umRepo.Upsert(r.Context(), &repository.UserMetadata{
		UserID:    userID,
		FileHash:  req.FileHash,
		TrackMBID: req.TrackMBID,
		Title:     req.Title,
		Artist:    artistStr,
		Album:     req.Album,
		Year:      req.Year,
		Genre:     req.Genre,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Immediately update all metadata in DB
	if req.TrackID != "" {
		if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
			hadMBID := track.MBID != ""
			if req.TrackMBID != "" { track.MBID = req.TrackMBID }
			if !hadMBID && req.Title != "" { track.Title = req.Title }
			h.trackRepo.Update(r.Context(), track)

			// Update artists association
			if len(req.Artists) == 0 {
				req.Artists = []struct {
					Name string `json:"name"`
					MBID string `json:"mbid"`
				}{{Name: "Unknown Artist"}}
			}
			var newArtists []*domain.TrackArtist
			for i, ar := range req.Artists {
				a := h.findOrCreateArtist(r.Context(), ar.Name, ar.MBID)
				if a == nil {
					continue
				}
				newArtists = append(newArtists, &domain.TrackArtist{
					ArtistID:  a.ID,
					Role:      "performer",
					SortOrder: i,
					Artist:    a,
				})
			}
			if len(newArtists) > 0 {
				h.trackRepo.ReplaceTrackArtists(r.Context(), track.ID, newArtists)
			}

			if !hadMBID && req.Album != "" && track.AlbumID != "" {
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

func (h *MetadataHandler) SearchTrack(w http.ResponseWriter, r *http.Request) {
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
		// Also load artist MBIDs from track_artists
		var artists []map[string]interface{}
		if req.TrackID != "" {
			if tas, err := h.trackRepo.LoadTrackArtists(r.Context(), req.TrackID); err == nil {
				for _, ta := range tas {
					artists = append(artists, map[string]interface{}{
						"name":      ta.Artist.Name,
						"artist_id": ta.ArtistID,
						"mbid":      ta.Artist.MBID,
						"role":      ta.Role,
					})
				}
			}
		}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"matched":      true,
		"cached":       true,
		"file_hash":    fileHash,
		"title":        cached.Title,
		"album":        cached.Album,
		"year":         cached.Year,
		"genre":        cached.Genre,
		"track_mbid":   cached.TrackMBID,
		"artists":        artists,
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
		// Include existing DB values so the form is pre-filled
		resp := map[string]interface{}{"matched": false, "file_hash": fileHash}
		if req.TrackID != "" {
			if track, err := h.trackRepo.FindByID(r.Context(), req.TrackID); err == nil {
				resp["title"] = track.Title
				if album, err := h.albumRepo.FindByID(r.Context(), track.AlbumID); err == nil {
					resp["album"] = album.Title
					resp["year"] = album.Year
					resp["genre"] = album.Genre
				}
				// Include cached user metadata as fallback
				if cached != nil {
					if cached.Title != "" { resp["title"] = cached.Title }
					if cached.Artist != "" { resp["artist"] = cached.Artist }
					if cached.Album != "" { resp["album"] = cached.Album }
					if cached.Year != 0 { resp["year"] = cached.Year }
					if cached.Genre != "" { resp["genre"] = cached.Genre }
				}
			}
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Merge: cached data has priority, MB fills gaps
	t := result.Title
	if t == "" { t = req.Title }
	if cached != nil && cached.Title != "" { t = cached.Title }
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

	// Build artists array from enrichment result
	var artists []map[string]interface{}
	for _, ar := range result.Artists {
		artists = append(artists, map[string]interface{}{
			"name":    ar.Name,
			"mbid":    ar.MBID,
			"role":    "performer",
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"matched":    true,
		"file_hash":  fileHash,
		"title":      t,
		"album":      al,
		"year":       yr,
		"genre":      gen,
		"track_mbid": tmbid,
		"album_mbid": result.AlbumMBID,
		"artists":      artists,
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
		if tas, err := h.trackRepo.LoadTrackArtists(r.Context(), track.ID); err == nil && len(tas) > 0 {
			if artist, err := h.artistRepo.FindByID(r.Context(), tas[0].ArtistID); err == nil && artist.MBID == "" {
				artist.MBID = result.ArtistMBID
				h.artistRepo.Update(r.Context(), artist)
			}
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

func (h *MetadataHandler) SearchArtist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	resolver := metadata.NewResolver(h.mbCfg)
	defer resolver.Close()
	client := metadata.NewMBClient(h.mbCfg)
	artists, _ := client.SearchArtists(req.Name)
	var result []map[string]interface{}
	for _, a := range artists {
		result = append(result, map[string]interface{}{
			"name":    a.Name,
			"mbid":    a.ID,
			"country": a.Country,
			"type":    a.Type,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"artists": result})
}

func (h *MetadataHandler) findOrCreateArtist(ctx context.Context, name string, mbid string) *domain.Artist {
	// If MBID is provided, try to look up by MBID first and fetch country
	if mbid != "" {
		if a, err := h.artistRepo.FindByMBID(ctx, mbid); err == nil {
			if a.Name == "" || a.Name == "Unknown Artist" {
				a.Name = name
				a.SortName = name
			}
			if a.Country == "" {
				full, err := metadata.NewMBClient(h.mbCfg).LookupArtist(mbid)
				if err == nil && full.Country != "" {
					a.Country = full.Country
				}
			}
			h.artistRepo.Update(ctx, a)
			return a
		}
	}
	// Look up by name
	a, err := h.artistRepo.FindByName(ctx, name)
	if err != nil {
		a = &domain.Artist{
			ID:        domain.NewID(),
			Name:      name,
			SortName:  name,
			MBID:      mbid,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if mbid != "" {
			full, err := metadata.NewMBClient(h.mbCfg).LookupArtist(mbid)
			if err == nil {
				a.Country = full.Country
				if a.Name == "" || a.Name == "Unknown Artist" {
					a.Name = full.Name
					a.SortName = full.SortName
				}
			}
		}
		h.artistRepo.BatchCreate(ctx, []domain.Artist{*a})
		a, _ = h.artistRepo.FindByName(ctx, name)
	} else if mbid != "" && a.MBID == "" {
		a.MBID = mbid
		if a.Country == "" {
			full, err := metadata.NewMBClient(h.mbCfg).LookupArtist(mbid)
			if err == nil && full.Country != "" {
				a.Country = full.Country
			}
		}
		h.artistRepo.Update(ctx, a)
	}
	return a
}
