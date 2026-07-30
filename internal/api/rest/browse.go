package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type DataHandler struct {
	db         *sql.DB
	trackRepo  *repository.TrackRepo
	albumRepo  *repository.AlbumRepo
	artistRepo *repository.ArtistRepo
	perm       *middleware.PermissionChecker
}

func NewDataHandler(db *sql.DB) *DataHandler {
	return &DataHandler{
		db:         db,
		trackRepo:  repository.NewTrackRepo(db),
		albumRepo:  repository.NewAlbumRepo(db),
		artistRepo: repository.NewArtistRepo(db),
		perm:       middleware.NewPermissionChecker(db),
	}
}

type pageInfo struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

func parsePagination(r *http.Request) (page, perPage int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ = strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	return
}

func (h *DataHandler) Tracks(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	libID := mux.Vars(r)["libId"]
	if libID != "" && libID != "__all__" && !h.perm.IsMember(r.Context(), libID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	page, perPage := parsePagination(r)

	allTracks, err := h.trackRepo.FindByLibraryID(r.Context(), libID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load tracks"})
		return
	}

	total := len(allTracks)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}

	items := allTracks[start:end]
	result := make([]map[string]interface{}, len(items))
	for i, t := range items {
		entry := map[string]interface{}{
			"id":             t.ID,
			"title":          t.Title,
			"cover_image_id": t.CoverImageID,
			"duration":       t.Duration,
			"bit_rate":       t.BitRate,
			"suffix":         t.FileFormat,
			"file_size":      t.FileSize,
			"file_hash":      t.Hash,
			"file_name":      filepath.Base(t.FilePath),
			"mbid":           t.MBID,
		}
		entry["albums"] = h.buildTrackAlbums(r.Context(), t.ID)
		entry["artists"] = h.buildTrackArtists(r.Context(), t.ID)
		result[i] = entry
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":    result,
		"page":     page,
		"per_page": perPage,
		"total":    total,
	})
}

func (h *DataHandler) Artists(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	all, err := h.artistRepo.FindAccessible(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load artists"})
		return
	}

	page, perPage := parsePagination(r)
	total := len(all)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":    all[start:end],
		"page":     page,
		"per_page": perPage,
		"total":    total,
	})
}

func (h *DataHandler) ArtistDetail(w http.ResponseWriter, r *http.Request) {

	artistID := mux.Vars(r)["artistId"]
	artist, err := h.artistRepo.FindByID(r.Context(), artistID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artist not found"})
		return
	}

	albums, _ := h.albumRepo.FindByArtistID(r.Context(), artistID)
	tracks, _ := h.trackRepo.FindByArtistID(r.Context(), artistID)

	trackList := make([]map[string]interface{}, len(tracks))
	for i, t := range tracks {
		trackList[i] = map[string]interface{}{
			"id":             t.ID,
			"title":          t.Title,
			"cover_image_id": t.CoverImageID,
			"duration":       t.Duration,
			"file_format":    t.FileFormat,
			"artists":        h.buildTrackArtists(r.Context(), t.ID),
			"albums":         h.buildTrackAlbums(r.Context(), t.ID),
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"artist":  artist,
		"albums":  albums,
		"tracks":  trackList,
	})
}

func (h *DataHandler) Albums(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	all, err := h.albumRepo.FindAccessible(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load albums"})
		return
	}

	page, perPage := parsePagination(r)
	total := len(all)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":    all[start:end],
		"page":     page,
		"per_page": perPage,
		"total":    total,
	})
}

func (h *DataHandler) AlbumDetail(w http.ResponseWriter, r *http.Request) {

	albumID := mux.Vars(r)["albumId"]
	album, err := h.albumRepo.FindByID(r.Context(), albumID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "album not found"})
		return
	}

	artist, _ := h.artistRepo.FindByID(r.Context(), album.ArtistID)
	artistName := ""
	if artist != nil {
		artistName = artist.Name
	}

	tracks, _ := h.trackRepo.FindByAlbumID(r.Context(), albumID)
	trackList := make([]map[string]interface{}, len(tracks))
	for i, t := range tracks {
		trackList[i] = map[string]interface{}{
			"id":             t.ID,
			"title":          t.Title,
			"cover_image_id": t.CoverImageID,
			"duration":       t.Duration,
			"file_format":    t.FileFormat,
			"artists":        h.buildTrackArtists(r.Context(), t.ID),
			"albums":         h.buildTrackAlbums(r.Context(), t.ID),
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"album": map[string]interface{}{
			"id":             album.ID,
			"title":          album.Title,
			"artist":         artistName,
			"artist_id":      album.ArtistID,
			"year":           album.Year,
			"genre":          album.Genre,
			"country":        album.Country,
			"duration":       album.Duration,
			"cover_image_id": album.CoverImageID,
		},
		"tracks": trackList,
	})
}

func (h *DataHandler) buildTrackAlbums(ctx context.Context, trackID string) []map[string]interface{} {
	tals, err := h.trackRepo.LoadTrackAlbums(ctx, trackID)
	if err != nil || len(tals) == 0 {
		return []map[string]interface{}{}
	}
	result := make([]map[string]interface{}, len(tals))
	for i, tal := range tals {
		entry := map[string]interface{}{
			"id":           tal.AlbumID,
			"track":        tal.TrackNumber,
			"disc_number":  tal.DiscNumber,
		}
		if tal.Album != nil {
			entry["title"] = tal.Album.Title
			entry["cover_image_id"] = tal.Album.CoverImageID
		}
		result[i] = entry
	}
	return result
}

func (h *DataHandler) buildTrackArtists(ctx context.Context, trackID string) []map[string]interface{} {
	tas, err := h.trackRepo.LoadTrackArtists(ctx, trackID)
	if err != nil || len(tas) == 0 {
		return []map[string]interface{}{}
	}
	result := make([]map[string]interface{}, len(tas))
	for i, ta := range tas {
		entry := map[string]interface{}{
			"artist_id": ta.ArtistID,
			"role":      ta.Role,
		}
		if ta.Artist != nil {
			entry["name"] = ta.Artist.Name
			entry["mbid"] = ta.Artist.MBID
		}
		result[i] = entry
	}
	return result
}

func (h *DataHandler) TracksByIDs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if len(req.IDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"tracks": []interface{}{}})
		return
	}

	tracks, err := h.trackRepo.FindByIDs(r.Context(), req.IDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if tracks == nil {
		tracks = []*domain.Track{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"tracks": tracks})
}
