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
	db          *sql.DB
	trackRepo   *repository.TrackRepo
	albumRepo   *repository.AlbumRepo
	artistRepo  *repository.ArtistRepo
	libraryRepo *repository.LibraryRepo
	perm        *middleware.PermissionChecker
}

func NewDataHandler(db *sql.DB) *DataHandler {
	return &DataHandler{
		db:          db,
		trackRepo:   repository.NewTrackRepo(db),
		albumRepo:   repository.NewAlbumRepo(db),
		artistRepo:  repository.NewArtistRepo(db),
		libraryRepo: repository.NewLibraryRepo(db),
		perm:        middleware.NewPermissionChecker(db),
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

	showAll := r.URL.Query().Get("all") == "1"
	var filtered []domain.Track
	for _, t := range allTracks {
		if showAll || t.Version <= 1 {
			filtered = append(filtered, t)
		}
	}

	total := len(filtered)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}

	items := filtered[start:end]

	var defaultMbids []string
	for _, t := range items {
		if t.Version == 1 && t.MBID != "" {
			defaultMbids = append(defaultMbids, t.MBID)
		}
	}

	var versionsByMbid map[string][]domain.Track
	if len(defaultMbids) > 0 {
		versionsByMbid = h.loadVersionMaps(r.Context(), defaultMbids, userID)
	}

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
			"version":        t.Version,
			"version_label":  t.VersionLabel,
		}
		entry["albums"] = h.buildTrackAlbums(r.Context(), t.ID)
		entry["artists"] = h.buildTrackArtists(r.Context(), t.ID)

		if t.Version == 1 && t.MBID != "" && versionsByMbid != nil {
			if siblings, ok := versionsByMbid[t.MBID]; ok {
				if versionList := buildVersionList(siblings, t.ID); len(versionList) > 0 {
					entry["versions"] = versionList
				}
			}
		}

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
	userID := middleware.GetUserID(r.Context())
	artistID := mux.Vars(r)["artistId"]
	artist, err := h.artistRepo.FindByID(r.Context(), artistID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artist not found"})
		return
	}

	albums, _ := h.albumRepo.FindByArtistID(r.Context(), artistID)
	tracks, _ := h.trackRepo.FindByArtistID(r.Context(), artistID)

	var trackList []map[string]interface{}
	var defaultMbids []string
	for _, t := range tracks {
		if t.Version >= 2 {
			continue
		}
		if t.Version == 1 && t.MBID != "" {
			defaultMbids = append(defaultMbids, t.MBID)
		}
		entry := map[string]interface{}{
			"id":             t.ID,
			"title":          t.Title,
			"cover_image_id": t.CoverImageID,
			"duration":       t.Duration,
			"file_format":    t.FileFormat,
			"version":        t.Version,
			"version_label":  t.VersionLabel,
			"mbid":           t.MBID,
			"artists":        h.buildTrackArtists(r.Context(), t.ID),
			"albums":         h.buildTrackAlbums(r.Context(), t.ID),
		}
		trackList = append(trackList, entry)
	}

	if len(defaultMbids) > 0 {
		versionsByMbid := h.loadVersionMaps(r.Context(), defaultMbids, userID)
		for _, entry := range trackList {
			if v, _ := entry["version"].(int); v != 1 {
				continue
			}
			mbid, _ := entry["mbid"].(string)
			if mbid == "" {
				continue
			}
			if siblings, ok := versionsByMbid[mbid]; ok {
				versionList := buildVersionList(siblings, entry["id"].(string))
				if len(versionList) > 0 {
					entry["versions"] = versionList
				}
			}
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
	userID := middleware.GetUserID(r.Context())
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
	var trackList []map[string]interface{}
	var defaultMbids []string
	for _, t := range tracks {
		if t.Version >= 2 {
			continue
		}
		if t.Version == 1 && t.MBID != "" {
			defaultMbids = append(defaultMbids, t.MBID)
		}
		entry := map[string]interface{}{
			"id":             t.ID,
			"title":          t.Title,
			"cover_image_id": t.CoverImageID,
			"duration":       t.Duration,
			"file_format":    t.FileFormat,
			"version":        t.Version,
			"version_label":  t.VersionLabel,
			"mbid":           t.MBID,
			"artists":        h.buildTrackArtists(r.Context(), t.ID),
			"albums":         h.buildTrackAlbums(r.Context(), t.ID),
		}
		trackList = append(trackList, entry)
	}

	if len(defaultMbids) > 0 {
		versionsByMbid := h.loadVersionMaps(r.Context(), defaultMbids, userID)
		for _, entry := range trackList {
			if v, _ := entry["version"].(int); v != 1 {
				continue
			}
			mbid, _ := entry["mbid"].(string)
			if mbid == "" {
				continue
			}
			if siblings, ok := versionsByMbid[mbid]; ok {
				versionList := buildVersionList(siblings, entry["id"].(string))
				if len(versionList) > 0 {
					entry["versions"] = versionList
				}
			}
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

func (h *DataHandler) loadVersionMaps(ctx context.Context, mbids []string, userID string) map[string][]domain.Track {
	if len(mbids) == 0 || userID == "" {
		return nil
	}
	accessibleLibs, _ := h.libraryRepo.FindByUserID(ctx, userID)
	var libIDs []string
	for _, l := range accessibleLibs {
		libIDs = append(libIDs, l.ID)
	}
	versionsByMbid, _ := h.trackRepo.FindVersionsByMbidBulk(ctx, mbids, libIDs)
	return versionsByMbid
}

func buildVersionList(siblings []domain.Track, excludeID string) []map[string]interface{} {
	var list []map[string]interface{}
	for _, s := range siblings {
		if s.ID == excludeID {
			continue
		}
		list = append(list, map[string]interface{}{
			"id":            s.ID,
			"version":       s.Version,
			"version_label": s.VersionLabel,
			"suffix":        s.FileFormat,
			"bit_rate":      s.BitRate,
			"duration":      s.Duration,
			"library_id":    s.LibraryID,
		})
	}
	return list
}
