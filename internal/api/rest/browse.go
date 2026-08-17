package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/lib/pq"
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
	pageStr := r.URL.Query().Get("page")
	if pageStr == "" {
		page = 1
	} else {
		page, _ = strconv.Atoi(pageStr)
		if page < 1 {
			page = 1
		}
	}
	perPageStr := r.URL.Query().Get("per_page")
	if perPageStr == "" {
		perPage = 10
	} else {
		perPage, _ = strconv.Atoi(perPageStr)
		if perPage < 1 {
			perPage = 0
		}
		if perPage > 100 {
			perPage = 100
		}
	}
	return
}

func (h *DataHandler) Tracks(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	page, perPage := parsePagination(r)
	if perPage < 1 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"items": []interface{}{}, "page": page, "per_page": perPage, "total": 0,
		})
		return
	}

	var allTracks []domain.Track
	var err error
	if libID := r.URL.Query().Get("libId"); libID != "" {
		if !h.perm.IsMember(r.Context(), libID, userID) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
			return
		}
		allTracks, err = h.trackRepo.FindByLibraryID(r.Context(), libID)
	} else {
		libs, _ := h.libraryRepo.FindByUserID(r.Context(), userID)
		var ids []string
		for _, l := range libs {
			ids = append(ids, l.ID)
		}
		allTracks, err = h.trackRepo.FindByLibraryID(r.Context(), ids...)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load tracks"})
		return
	}

	showAll := r.URL.Query().Get("all") == "1"
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	qLower := strings.ToLower(q)

	var filtered []domain.Track
	for _, t := range allTracks {
		if !showAll && t.Version > 1 {
			continue
		}
		if qLower != "" && !strings.Contains(strings.ToLower(t.Title), qLower) {
			continue
		}
		filtered = append(filtered, t)
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

	// Version lookup keys are deduped (metadata_source, external_id) pairs:
	// a bare external_id repeated across tracks or reused by another source
	// would otherwise inflate the bulk query with rows destined to be thrown
	// away (see buildVersionListFor's source filter).
	versionKeys := make(map[repository.VersionGroupKey]struct{})
	for _, t := range items {
		if t.Version == 1 && t.ExternalID != "" {
			versionKeys[repository.VersionGroupKey{MetadataSource: t.MetadataSource, ExternalID: t.ExternalID}] = struct{}{}
		}
	}

	var versionsByExternalID map[repository.VersionGroupKey][]domain.Track
	if len(versionKeys) > 0 {
		versionsByExternalID = h.loadVersionMaps(r.Context(), versionKeySlice(versionKeys), userID)
	}

	result := make([]map[string]interface{}, len(items))
	for i, t := range items {
		entry := map[string]interface{}{
			"id":              t.ID,
			"title":           t.Title,
			"cover_image_id":  t.CoverImageID,
			"duration":        t.Duration,
			"bit_rate":        t.BitRate,
			"suffix":          t.FileFormat,
			"file_size":       t.FileSize,
			"file_hash":       t.Hash,
			"file_name":       filepath.Base(t.FilePath),
			"external_id":     t.ExternalID,
			"version":         t.Version,
			"version_label":   t.VersionLabel,
			"metadata_source": t.MetadataSource,
		}
		entry["albums"] = h.buildTrackAlbums(r.Context(), t.ID)
		entry["artists"] = h.buildTrackArtists(r.Context(), t.ID)

		if t.Version == 1 && t.ExternalID != "" && versionsByExternalID != nil {
			if siblings, ok := versionsByExternalID[repository.VersionGroupKey{MetadataSource: t.MetadataSource, ExternalID: t.ExternalID}]; ok {
				if versionList := buildVersionListFor(siblings, t.MetadataSource, t.ID); len(versionList) > 0 {
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

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		qLower := strings.ToLower(q)
		filtered := make([]domain.Artist, 0)
		for _, a := range all {
			if strings.Contains(strings.ToLower(a.Name), qLower) {
				filtered = append(filtered, a)
			}
		}
		all = filtered
	}

	page, perPage := parsePagination(r)
	if perPage < 1 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"items": []interface{}{}, "page": page, "per_page": perPage, "total": 0,
		})
		return
	}
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
	page, perPage := parsePagination(r)
	if perPage < 1 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"artist": nil, "albums": []interface{}{}, "tracks": []interface{}{},
			"page": page, "per_page": perPage, "total": 0,
		})
		return
	}

	artist, err := h.artistRepo.FindByID(r.Context(), artistID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "artist not found"})
		return
	}

	albums, _ := h.albumRepo.FindByArtistID(r.Context(), artistID)
	tracks, _ := h.trackRepo.FindByArtistID(r.Context(), artistID)

	var trackList []map[string]interface{}
	versionKeys := make(map[repository.VersionGroupKey]struct{})
	for _, t := range tracks {
		if t.Version >= 2 {
			continue
		}
		if t.Version == 1 && t.ExternalID != "" {
			versionKeys[repository.VersionGroupKey{MetadataSource: t.MetadataSource, ExternalID: t.ExternalID}] = struct{}{}
		}
		entry := map[string]interface{}{
			"id":              t.ID,
			"title":           t.Title,
			"cover_image_id":  t.CoverImageID,
			"duration":        t.Duration,
			"file_format":     t.FileFormat,
			"version":         t.Version,
			"version_label":   t.VersionLabel,
			"metadata_source": t.MetadataSource,
			"external_id":     t.ExternalID,
			"artists":         h.buildTrackArtists(r.Context(), t.ID),
			"albums":          h.buildTrackAlbums(r.Context(), t.ID),
		}
		trackList = append(trackList, entry)
	}

	if len(versionKeys) > 0 {
		versionsByExternalID := h.loadVersionMaps(r.Context(), versionKeySlice(versionKeys), userID)
		for _, entry := range trackList {
			if v, _ := entry["version"].(int); v != 1 {
				continue
			}
			extID, _ := entry["external_id"].(string)
			if extID == "" {
				continue
			}
			src, _ := entry["metadata_source"].(string)
			if siblings, ok := versionsByExternalID[repository.VersionGroupKey{MetadataSource: src, ExternalID: extID}]; ok {
				versionList := buildVersionListFor(siblings, src, entry["id"].(string))
				if len(versionList) > 0 {
					entry["versions"] = versionList
				}
			}
		}
	}

	total := len(trackList)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"artist":   artist,
		"albums":   albums,
		"tracks":   trackList[start:end],
		"page":     page,
		"per_page": perPage,
		"total":    total,
	})
}

func (h *DataHandler) Albums(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	all, err := h.albumRepo.FindAccessible(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load albums"})
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		qLower := strings.ToLower(q)
		filtered := make([]domain.Album, 0)
		for _, a := range all {
			if strings.Contains(strings.ToLower(a.Title), qLower) {
				filtered = append(filtered, a)
			}
		}
		all = filtered
	}

	page, perPage := parsePagination(r)
	if perPage < 1 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"items": []interface{}{}, "page": page, "per_page": perPage, "total": 0,
		})
		return
	}
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
	page, perPage := parsePagination(r)
	if perPage < 1 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"album": nil, "tracks": []interface{}{},
			"page": page, "per_page": perPage, "total": 0,
		})
		return
	}

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
	versionKeys := make(map[repository.VersionGroupKey]struct{})
	for _, t := range tracks {
		if t.Version >= 2 {
			continue
		}
		if t.Version == 1 && t.ExternalID != "" {
			versionKeys[repository.VersionGroupKey{MetadataSource: t.MetadataSource, ExternalID: t.ExternalID}] = struct{}{}
		}
		entry := map[string]interface{}{
			"id":              t.ID,
			"title":           t.Title,
			"cover_image_id":  t.CoverImageID,
			"duration":        t.Duration,
			"file_format":     t.FileFormat,
			"version":         t.Version,
			"version_label":   t.VersionLabel,
			"metadata_source": t.MetadataSource,
			"external_id":     t.ExternalID,
			"artists":         h.buildTrackArtists(r.Context(), t.ID),
			"albums":          h.buildTrackAlbums(r.Context(), t.ID),
		}
		trackList = append(trackList, entry)
	}

	if len(versionKeys) > 0 {
		versionsByExternalID := h.loadVersionMaps(r.Context(), versionKeySlice(versionKeys), userID)
		for _, entry := range trackList {
			if v, _ := entry["version"].(int); v != 1 {
				continue
			}
			extID, _ := entry["external_id"].(string)
			if extID == "" {
				continue
			}
			src, _ := entry["metadata_source"].(string)
			if siblings, ok := versionsByExternalID[repository.VersionGroupKey{MetadataSource: src, ExternalID: extID}]; ok {
				versionList := buildVersionListFor(siblings, src, entry["id"].(string))
				if len(versionList) > 0 {
					entry["versions"] = versionList
				}
			}
		}
	}

	total := len(trackList)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
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
		"tracks":   trackList[start:end],
		"page":     page,
		"per_page": perPage,
		"total":    total,
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
			"id":          tal.AlbumID,
			"track":       tal.TrackNumber,
			"disc_number": tal.DiscNumber,
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
			entry["external_id"] = ta.Artist.ExternalID
			// external_id alone is ambiguous across namespaces (MusicBrainz
			// UUIDs vs NetEase numeric ids); consumers need the source to
			// interpret it, matching how track entries pair the two.
			entry["metadata_source"] = ta.Artist.MetadataSource
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

func (h *DataHandler) loadVersionMaps(ctx context.Context, keys []repository.VersionGroupKey, userID string) map[repository.VersionGroupKey][]domain.Track {
	if len(keys) == 0 || userID == "" {
		return nil
	}
	accessibleLibs, err := h.libraryRepo.FindByUserID(ctx, userID)
	if err != nil {
		log.Printf("[browse] load accessible libraries for versions: %v", err)
		return nil
	}
	var libIDs []string
	for _, l := range accessibleLibs {
		libIDs = append(libIDs, l.ID)
	}
	versionsByExternalID, err := h.trackRepo.FindVersionsByExternalIDBulk(ctx, keys, libIDs)
	if err != nil {
		// Degrade gracefully (versions silently disappear) but keep the
		// failure observable.
		log.Printf("[browse] load version groups: %v", err)
		return nil
	}
	return versionsByExternalID
}

// versionKeySlice flattens a deduped (metadata_source, external_id) key set
// into the slice form the bulk version lookup consumes.
func versionKeySlice(m map[repository.VersionGroupKey]struct{}) []repository.VersionGroupKey {
	keys := make([]repository.VersionGroupKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// buildVersionListFor renders a track's "other versions" from its version
// group. Only siblings sharing the track's metadata source participate: the
// version-group identity is (metadata_source, external_id), and two sources
// can legitimately use the same external_id value for different works.
func buildVersionListFor(siblings []domain.Track, source, excludeID string) []map[string]interface{} {
	var list []map[string]interface{}
	for _, s := range siblings {
		if s.ID == excludeID || s.MetadataSource != source {
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

func (h *DataHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) < 2 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"tracks":  []interface{}{},
			"albums":  []interface{}{},
			"artists": []interface{}{},
		})
		return
	}

	userID := middleware.GetUserID(r.Context())
	libs, _ := h.libraryRepo.FindByUserID(r.Context(), userID)
	libIDs := make([]string, len(libs))
	for i, l := range libs {
		libIDs[i] = l.ID
	}
	if len(libIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"tracks":  []interface{}{},
			"albums":  []interface{}{},
			"artists": []interface{}{},
		})
		return
	}

	qLike := "%" + q + "%"

	// Search tracks
	trackRows, _ := h.db.QueryContext(r.Context(),
		`SELECT t.id, t.title, t.duration, t.file_format, t.cover_image_id, t.version, t.version_label
		 FROM tracks t WHERE t.library_id = ANY($1) AND t.title ILIKE $2 AND t.version <= 1
		 ORDER BY t.title LIMIT 10`, pq.Array(libIDs), qLike)
	var trackList []map[string]interface{}
	for trackRows != nil && trackRows.Next() {
		var id, title, fileFormat, versionLabel string
		var duration float64
		var coverID sql.NullString
		var version int
		trackRows.Scan(&id, &title, &duration, &fileFormat, &coverID, &version, &versionLabel)
		entry := map[string]interface{}{
			"id": id, "title": title, "duration": duration,
			"file_format": fileFormat, "version": version, "version_label": versionLabel,
		}
		if coverID.Valid {
			entry["cover_image_id"] = coverID.String
		}
		entry["artists"] = h.buildTrackArtists(r.Context(), id)
		entry["albums"] = h.buildTrackAlbums(r.Context(), id)
		trackList = append(trackList, entry)
	}
	if trackRows != nil {
		trackRows.Close()
	}

	// Search albums
	albumRows, _ := h.db.QueryContext(r.Context(),
		`SELECT a.id, a.title, a.year, a.cover_image_id
		 FROM albums a WHERE a.library_id = ANY($1) AND a.title ILIKE $2
		 ORDER BY a.title LIMIT 10`, pq.Array(libIDs), qLike)
	var albumList []map[string]interface{}
	for albumRows != nil && albumRows.Next() {
		var id, title string
		var year int
		var coverID sql.NullString
		albumRows.Scan(&id, &title, &year, &coverID)
		entry := map[string]interface{}{"id": id, "title": title, "year": year}
		if coverID.Valid {
			entry["cover_image_id"] = coverID.String
		}
		albumList = append(albumList, entry)
	}
	if albumRows != nil {
		albumRows.Close()
	}

	// Search artists
	artistRows, _ := h.db.QueryContext(r.Context(),
		`SELECT a.id, a.name, a.cover_image_id
		 FROM artists a WHERE a.library_id = ANY($1) AND a.name ILIKE $2
		 ORDER BY a.name LIMIT 10`, pq.Array(libIDs), qLike)
	var artistList []map[string]interface{}
	for artistRows != nil && artistRows.Next() {
		var id, name string
		var coverID sql.NullString
		artistRows.Scan(&id, &name, &coverID)
		entry := map[string]interface{}{"id": id, "name": name}
		if coverID.Valid {
			entry["cover_image_id"] = coverID.String
		}
		artistList = append(artistList, entry)
	}
	if artistRows != nil {
		artistRows.Close()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tracks":  trackList,
		"albums":  albumList,
		"artists": artistList,
	})
}
