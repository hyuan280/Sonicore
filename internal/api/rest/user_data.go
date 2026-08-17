package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/lib/pq"
	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type UserDataHandler struct {
	db          *sql.DB
	trackRepo   *repository.TrackRepo
	artistRepo  *repository.ArtistRepo
	albumRepo   *repository.AlbumRepo
	libraryRepo *repository.LibraryRepo
}

func NewUserDataHandler(db *sql.DB) *UserDataHandler {
	return &UserDataHandler{
		db:          db,
		trackRepo:   repository.NewTrackRepo(db),
		artistRepo:  repository.NewArtistRepo(db),
		albumRepo:   repository.NewAlbumRepo(db),
		libraryRepo: repository.NewLibraryRepo(db),
	}
}

// ---- Favorites ----

func (h *UserDataHandler) ListFavorites(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	itemType := r.URL.Query().Get("type")
	page, perPage := parsePagination(r)
	if perPage < 1 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"items": []interface{}{}, "page": page, "per_page": perPage, "total": 0,
		})
		return
	}

	var items []map[string]interface{}
	var total int

	q := strings.TrimSpace(r.URL.Query().Get("q"))

	if itemType == "track" {
		var trackTotal int
		if q != "" {
			h.db.QueryRowContext(r.Context(),
				`SELECT COUNT(DISTINCT CASE WHEN t.external_id IS NULL OR t.external_id = '' THEN (f.item_id::text, ''::text) ELSE (t.metadata_source::text, t.external_id::text) END)
				 FROM favorites f
				 LEFT JOIN tracks t ON t.id = f.item_id AND f.item_type = 'track'
				 WHERE f.user_id = $1 AND f.item_type = 'track' AND t.version <= 1 AND t.title ILIKE $2`, userID, "%"+q+"%").Scan(&trackTotal)
		} else {
			h.db.QueryRowContext(r.Context(),
				`SELECT COUNT(DISTINCT CASE WHEN t.external_id IS NULL OR t.external_id = '' THEN (f.item_id::text, ''::text) ELSE (t.metadata_source::text, t.external_id::text) END)
				 FROM favorites f
				 LEFT JOIN tracks t ON t.id = f.item_id AND f.item_type = 'track'
				 WHERE f.user_id = $1 AND f.item_type = 'track' AND t.version <= 1`, userID).Scan(&trackTotal)
		}
		total = trackTotal

		offset := (page - 1) * perPage
		var rows *sql.Rows
		var err error
		if q != "" {
			rows, err = h.db.QueryContext(r.Context(),
				`SELECT DISTINCT ON (CASE WHEN t.external_id IS NULL OR t.external_id = '' THEN (f.item_id::text, ''::text) ELSE (t.metadata_source::text, t.external_id::text) END)
				        f.item_type, f.item_id, f.created_at,
				        COALESCE(t.title, ''), COALESCE(sub.album_title, ''),
				        COALESCE(sub.album_id, ''), COALESCE(t.duration, 0), COALESCE(t.file_format, ''),
				        t.cover_image_id, COALESCE(t.version, 0), COALESCE(t.version_label, ''), COALESCE(t.external_id, ''), COALESCE(t.metadata_source, '')
				 FROM favorites f
				 LEFT JOIN tracks t ON t.id = f.item_id AND f.item_type = 'track'
				 LEFT JOIN LATERAL (
				     SELECT tal.album_id, al.title AS album_title
				     FROM track_albums tal
				     JOIN albums al ON al.id = tal.album_id
				     WHERE tal.track_id = t.id
				     ORDER BY tal.disc_number, tal.track_number
				     LIMIT 1
				 ) sub ON true
				 WHERE f.user_id = $1 AND f.item_type = 'track' AND t.version <= 1 AND t.title ILIKE $2
				 ORDER BY CASE WHEN t.external_id IS NULL OR t.external_id = '' THEN (f.item_id::text, ''::text) ELSE (t.metadata_source::text, t.external_id::text) END, t.version ASC, f.created_at DESC
				 LIMIT $3 OFFSET $4`, userID, "%"+q+"%", perPage, offset)
		} else {
			rows, err = h.db.QueryContext(r.Context(),
				`SELECT DISTINCT ON (CASE WHEN t.external_id IS NULL OR t.external_id = '' THEN (f.item_id::text, ''::text) ELSE (t.metadata_source::text, t.external_id::text) END)
			        f.item_type, f.item_id, f.created_at,
			        COALESCE(t.title, ''), COALESCE(sub.album_title, ''),
			        COALESCE(sub.album_id, ''), COALESCE(t.duration, 0), COALESCE(t.file_format, ''),
			        t.cover_image_id, COALESCE(t.version, 0), COALESCE(t.version_label, ''), COALESCE(t.external_id, ''), COALESCE(t.metadata_source, '')
			 FROM favorites f
			 LEFT JOIN tracks t ON t.id = f.item_id AND f.item_type = 'track'
			 LEFT JOIN LATERAL (
			     SELECT tal.album_id, al.title AS album_title
			     FROM track_albums tal
			     JOIN albums al ON al.id = tal.album_id
			     WHERE tal.track_id = t.id
			     ORDER BY tal.disc_number, tal.track_number
			     LIMIT 1
			 ) sub ON true
			 WHERE f.user_id = $1 AND f.item_type = 'track' AND t.version <= 1
			 ORDER BY CASE WHEN t.external_id IS NULL OR t.external_id = '' THEN (f.item_id::text, ''::text) ELSE (t.metadata_source::text, t.external_id::text) END, t.version ASC, f.created_at DESC
			 LIMIT $2 OFFSET $3`, userID, perPage, offset)
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
			return
		}
		defer rows.Close()
		var trackIDs []string
		favKeys := make(map[repository.VersionGroupKey]struct{})
		for rows.Next() {
			var t, id string
			var ca time.Time
			var title, album, albumID, fileFormat string
			var duration float64
			var coverID sql.NullString
			var version int
			var versionLabel, extID, metaSource string
			rows.Scan(&t, &id, &ca, &title, &album, &albumID, &duration, &fileFormat, &coverID, &version, &versionLabel, &extID, &metaSource)
			item := map[string]interface{}{
				"item_type": t, "item_id": id, "created_at": ca,
				"title": title, "duration": duration, "suffix": fileFormat,
				"version": version, "version_label": versionLabel, "external_id": extID,
				"metadata_source": metaSource,
			}
			if albumID != "" {
				item["albums"] = []map[string]interface{}{{"id": albumID, "title": album}}
			}
			if coverID.Valid {
				item["cover_image_id"] = coverID.String
			}
			items = append(items, item)
			trackIDs = append(trackIDs, id)
			if extID != "" {
				favKeys[repository.VersionGroupKey{MetadataSource: metaSource, ExternalID: extID}] = struct{}{}
			}
		}
		artistsByTrack := h.loadTrackArtistsBulk(r.Context(), trackIDs)
		for i := range items {
			tid := items[i]["item_id"].(string)
			if artists, ok := artistsByTrack[tid]; ok {
				items[i]["artists"] = artists
			}
		}
		if len(favKeys) > 0 {
			accessibleLibs, _ := h.libraryRepo.FindByUserID(r.Context(), userID)
			var libIDs []string
			for _, l := range accessibleLibs {
				libIDs = append(libIDs, l.ID)
			}
			versionsByExtID, _ := h.trackRepo.FindVersionsByExternalIDBulk(r.Context(), versionKeySlice(favKeys), libIDs)
			if versionsByExtID != nil {
				for _, item := range items {
					extID, _ := item["external_id"].(string)
					if extID == "" {
						continue
					}
					src, _ := item["metadata_source"].(string)
					if siblings, ok := versionsByExtID[repository.VersionGroupKey{MetadataSource: src, ExternalID: extID}]; ok {
						itemID, _ := item["item_id"].(string)
						if versionList := buildVersionListFor(siblings, src, itemID); len(versionList) > 0 {
							item["versions"] = versionList
						}
					}
				}
			}
		}
	} else {
		rows, err := h.db.QueryContext(r.Context(),
			"SELECT item_type, item_id, created_at FROM favorites WHERE user_id = $1 AND ($2 = '' OR item_type = $2) ORDER BY created_at DESC",
			userID, itemType)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var t, id string
			var ca time.Time
			rows.Scan(&t, &id, &ca)
			items = append(items, map[string]interface{}{
				"item_type": t, "item_id": id, "created_at": ca,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items, "page": page, "per_page": perPage, "total": total,
	})
}

type favoritesRequest struct {
	ItemType string   `json:"item_type"`
	ItemIDs  []string `json:"item_ids"`
}

func (h *UserDataHandler) AddFavorites(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req favoritesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	now := time.Now()
	for _, id := range req.ItemIDs {
		ids := h.expandTrackVersions(r.Context(), userID, req.ItemType, []string{id})
		for _, tid := range ids {
			var libID *string
			if req.ItemType == "track" {
				h.db.QueryRowContext(r.Context(),
					"SELECT library_id FROM tracks WHERE id = $1", tid).Scan(&libID)
			}
			if _, err := h.db.ExecContext(r.Context(),
				`INSERT INTO favorites (user_id, item_type, item_id, library_id, created_at)
				 VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
				userID, req.ItemType, tid, libID, now); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "favorited"})
}

func (h *UserDataHandler) RemoveFavorites(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req favoritesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	for _, id := range req.ItemIDs {
		ids := h.expandTrackVersions(r.Context(), userID, req.ItemType, []string{id})
		for _, tid := range ids {
			h.db.ExecContext(r.Context(),
				"DELETE FROM favorites WHERE user_id = $1 AND item_type = $2 AND item_id = $3",
				userID, req.ItemType, tid)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (h *UserDataHandler) CheckFavorites(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	favSet := make(map[string]bool, len(req.IDs))
	if len(req.IDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"favorites": favSet})
		return
	}

	rows, err := h.db.QueryContext(r.Context(),
		"SELECT item_id FROM favorites WHERE user_id = $1 AND item_type = 'track' AND item_id = ANY($2)",
		userID, pq.Array(req.IDs))
	if err == nil {
		defer rows.Close()
		var fid string
		for rows.Next() {
			rows.Scan(&fid)
			favSet[fid] = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"favorites": favSet})
}

// ---- Play History ----

func (h *UserDataHandler) ListHistory(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	page, perPage := parsePagination(r)
	if perPage < 1 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"items": []interface{}{}, "page": page, "per_page": perPage, "total": 0,
		})
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))

	var total int
	if q != "" {
		h.db.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM play_history ph
			 INNER JOIN tracks t ON t.id = ph.track_id
			 WHERE ph.user_id = $1 AND t.title ILIKE $2`, userID, "%"+q+"%").Scan(&total)
	} else {
		h.db.QueryRowContext(r.Context(),
			"SELECT COUNT(*) FROM play_history WHERE user_id = $1", userID).Scan(&total)
	}

	offset := (page - 1) * perPage
	var rows *sql.Rows
	var err error
	if q != "" {
		rows, err = h.db.QueryContext(r.Context(),
			`SELECT ph.id, ph.track_id, ph.played_at,
			        COALESCE(t.title, ''), COALESCE(sub.album_title, ''),
			        COALESCE(sub.album_id, ''), COALESCE(t.duration, 0), COALESCE(t.file_format, ''),
			        t.cover_image_id
			 FROM play_history ph
			 INNER JOIN tracks t ON t.id = ph.track_id
			 LEFT JOIN LATERAL (
			     SELECT tal.album_id, al.title AS album_title
			     FROM track_albums tal
			     JOIN albums al ON al.id = tal.album_id
			     WHERE tal.track_id = t.id
			     ORDER BY tal.disc_number, tal.track_number
			     LIMIT 1
			 ) sub ON true
			 WHERE ph.user_id = $1 AND t.title ILIKE $2
			 ORDER BY ph.played_at DESC LIMIT $3 OFFSET $4`,
			userID, "%"+q+"%", perPage, offset)
	} else {
		rows, err = h.db.QueryContext(r.Context(),
			`SELECT ph.id, ph.track_id, ph.played_at,
			        COALESCE(t.title, ''), COALESCE(sub.album_title, ''),
			        COALESCE(sub.album_id, ''), COALESCE(t.duration, 0), COALESCE(t.file_format, ''),
			        t.cover_image_id
			 FROM play_history ph
			 INNER JOIN tracks t ON t.id = ph.track_id
			 LEFT JOIN LATERAL (
			     SELECT tal.album_id, al.title AS album_title
			     FROM track_albums tal
			     JOIN albums al ON al.id = tal.album_id
			     WHERE tal.track_id = t.id
			     ORDER BY tal.disc_number, tal.track_number
			     LIMIT 1
			 ) sub ON true
			 WHERE ph.user_id = $1
			 ORDER BY ph.played_at DESC LIMIT $2 OFFSET $3`,
			userID, perPage, offset)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	var items []map[string]interface{}
	var trackIDs []string
		for rows.Next() {
			var id, tid string
			var pa time.Time
			var title, album, albumID, fileFormat string
			var duration float64
			var coverID sql.NullString
			rows.Scan(&id, &tid, &pa, &title, &album,
				&albumID, &duration, &fileFormat, &coverID)
			item := map[string]interface{}{
				"id": id, "track_id": tid, "played_at": pa,
				"title": title, "duration": duration, "suffix": fileFormat,
			}
			if albumID != "" {
				item["albums"] = []map[string]interface{}{{"id": albumID, "title": album}}
			}
		if coverID.Valid {
			item["cover_image_id"] = coverID.String
		}
		items = append(items, item)
		trackIDs = append(trackIDs, tid)
	}
	artistsByTrack := h.loadTrackArtistsBulk(r.Context(), trackIDs)
	for i := range items {
		tid := items[i]["track_id"].(string)
		if artists, ok := artistsByTrack[tid]; ok {
			items[i]["artists"] = artists
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items, "page": page, "per_page": perPage, "total": total,
	})
}

func (h *UserDataHandler) AddHistory(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		TrackID string `json:"track_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	now := time.Now()
	h.db.ExecContext(r.Context(),
		`DELETE FROM play_history WHERE user_id=$1 AND track_id=$2`, userID, req.TrackID)

	var libID string
	err := h.db.QueryRowContext(r.Context(),
		"SELECT library_id FROM tracks WHERE id = $1", req.TrackID).Scan(&libID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "track not found"})
		return
	}

	if _, err := h.db.ExecContext(r.Context(),
		`INSERT INTO play_history (id, user_id, track_id, library_id, played_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		domain.NewID(), userID, req.TrackID, libID, now); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (h *UserDataHandler) RemoveHistoryItems(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	h.db.ExecContext(r.Context(),
		"DELETE FROM play_history WHERE id = ANY($1) AND user_id = $2",
		pq.Array(req.IDs), userID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---- Playlists ----

func (h *UserDataHandler) ListPlaylists(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	rows, err := h.db.QueryContext(r.Context(),
		"SELECT id, name, track_ids, is_public, created_at FROM playlists WHERE owner_id = $1 ORDER BY name",
		userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	type pl struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		TrackIDs  json.RawMessage `json:"track_ids"`
		IsPublic  bool            `json:"is_public"`
		CreatedAt time.Time       `json:"created_at"`
	}
	var list []pl
	for rows.Next() {
		var p pl
		rows.Scan(&p.ID, &p.Name, &p.TrackIDs, &p.IsPublic, &p.CreatedAt)
		list = append(list, p)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"items": list})
}

func (h *UserDataHandler) CreatePlaylist(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	now := time.Now()
	id := domain.NewID()
	_, err := h.db.ExecContext(r.Context(),
		"INSERT INTO playlists (id, name, owner_id, track_ids, is_public, created_at, updated_at) VALUES ($1, $2, $3, '[]', false, $4, $4)",
		id, req.Name, userID, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "create failed: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "created", "id": id})
}

func (h *UserDataHandler) GetPlaylist(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	plID := mux.Vars(r)["id"]
	page, perPage := parsePagination(r)
	all := r.URL.Query().Get("all") == "1"
	if !all && perPage < 1 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"id": plID, "name": "", "tracks": []interface{}{},
			"page": page, "per_page": perPage, "total": 0,
		})
		return
	}

	var name string
	var trackIDsJSON []byte
	var createdAt time.Time
	err := h.db.QueryRowContext(r.Context(),
		"SELECT name, track_ids, created_at FROM playlists WHERE id = $1 AND owner_id = $2",
		plID, userID).Scan(&name, &trackIDsJSON, &createdAt)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found"})
		return
	}

	var trackIDs []string
	json.Unmarshal(trackIDsJSON, &trackIDs)

	total := len(trackIDs)
	var pagedIDs []string
	if all {
		pagedIDs = trackIDs
	} else {
		start := (page - 1) * perPage
		if start > total {
			start = total
		}
		end := start + perPage
		if end > total {
			end = total
		}
		pagedIDs = trackIDs[start:end]
	}

	var tracks []map[string]interface{}
	versionKeys := make(map[repository.VersionGroupKey]struct{})
	for _, tid := range pagedIDs {
		track, err := h.trackRepo.FindByID(r.Context(), tid)
		if err != nil {
			continue
		}
		t := map[string]interface{}{
			"id":              track.ID,
			"title":           track.Title,
			"cover_image_id":  track.CoverImageID,
			"duration":        track.Duration,
			"bit_rate":        track.BitRate,
			"suffix":          track.FileFormat,
			"version":         track.Version,
			"version_label":   track.VersionLabel,
			"file_format":     track.FileFormat,
			"external_id":     track.ExternalID,
			"metadata_source": track.MetadataSource,
		}
		if track.ExternalID != "" {
			versionKeys[repository.VersionGroupKey{MetadataSource: track.MetadataSource, ExternalID: track.ExternalID}] = struct{}{}
		}
		if len(track.Albums) > 0 {
			albums := make([]map[string]interface{}, len(track.Albums))
			for i, tal := range track.Albums {
				entry := map[string]interface{}{
					"id": tal.AlbumID,
					"track": tal.TrackNumber,
					"disc_number": tal.DiscNumber,
				}
				if tal.Album != nil {
					entry["title"] = tal.Album.Title
				}
				albums[i] = entry
			}
			t["albums"] = albums
		}
		if tas, err := h.trackRepo.LoadTrackArtists(r.Context(), track.ID); err == nil && len(tas) > 0 {
			artists := make([]map[string]interface{}, len(tas))
			for i, ta := range tas {
				artists[i] = map[string]interface{}{
					"artist_id": ta.ArtistID,
					"name":      ta.Artist.Name,
					"role":      ta.Role,
				}
			}
			t["artists"] = artists
		}
		tracks = append(tracks, t)
	}

	if len(versionKeys) > 0 {
		accessibleLibs, _ := h.libraryRepo.FindByUserID(r.Context(), userID)
		var libIDs []string
		for _, l := range accessibleLibs {
			libIDs = append(libIDs, l.ID)
		}
		versionsByExtID, _ := h.trackRepo.FindVersionsByExternalIDBulk(r.Context(), versionKeySlice(versionKeys), libIDs)
		if versionsByExtID != nil {
			for _, t := range tracks {
				trackExtID, _ := t["external_id"].(string)
				if trackExtID == "" {
					continue
				}
				src, _ := t["metadata_source"].(string)
				if siblings, ok := versionsByExtID[repository.VersionGroupKey{MetadataSource: src, ExternalID: trackExtID}]; ok {
					trackID, _ := t["id"].(string)
					if versionList := buildVersionListFor(siblings, src, trackID); len(versionList) > 0 {
						t["versions"] = versionList
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": plID, "name": name, "tracks": tracks, "created_at": createdAt,
		"page": page, "per_page": perPage, "total": total,
	})
}

func (h *UserDataHandler) DeletePlaylist(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	plID := mux.Vars(r)["id"]

	h.db.ExecContext(r.Context(),
		"DELETE FROM playlists WHERE id = $1 AND owner_id = $2", plID, userID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *UserDataHandler) AddTrackToPlaylist(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	plID := mux.Vars(r)["id"]

	var req struct{ TrackID string `json:"track_id"` }
	json.NewDecoder(r.Body).Decode(&req)
	if req.TrackID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "track_id required"})
		return
	}

	var trackIDs []byte
	h.db.QueryRowContext(r.Context(),
		"SELECT track_ids FROM playlists WHERE id = $1 AND owner_id = $2", plID, userID).Scan(&trackIDs)

	var ids []string
	json.Unmarshal(trackIDs, &ids)
	for _, id := range ids {
		if id == req.TrackID {
			writeJSON(w, http.StatusOK, map[string]string{"status": "already exists"})
			return
		}
	}
	ids = append(ids, req.TrackID)
	updated, _ := json.Marshal(ids)

	h.db.ExecContext(r.Context(),
		"UPDATE playlists SET track_ids = $1, updated_at = NOW() WHERE id = $2 AND owner_id = $3",
		updated, plID, userID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

func (h *UserDataHandler) AddTracksToPlaylist(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	plID := mux.Vars(r)["id"]

	var req struct{ TrackIDs []string `json:"track_ids"` }
	json.NewDecoder(r.Body).Decode(&req)
	if len(req.TrackIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "track_ids required"})
		return
	}

	var trackIDs []byte
	h.db.QueryRowContext(r.Context(),
		"SELECT track_ids FROM playlists WHERE id = $1 AND owner_id = $2", plID, userID).Scan(&trackIDs)

	var ids []string
	json.Unmarshal(trackIDs, &ids)
	existing := make(map[string]bool, len(ids))
	for _, id := range ids {
		existing[id] = true
	}
	for _, id := range req.TrackIDs {
		if !existing[id] {
			ids = append(ids, id)
			existing[id] = true
		}
	}
	updated, _ := json.Marshal(ids)

	h.db.ExecContext(r.Context(),
		"UPDATE playlists SET track_ids = $1, updated_at = NOW() WHERE id = $2 AND owner_id = $3",
		updated, plID, userID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

func (h *UserDataHandler) RemoveTracksFromPlaylist(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	plID := mux.Vars(r)["id"]

	var req struct{ TrackIDs []string `json:"track_ids"` }
	json.NewDecoder(r.Body).Decode(&req)
	if len(req.TrackIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "track_ids required"})
		return
	}

	var trackIDs []byte
	h.db.QueryRowContext(r.Context(),
		"SELECT track_ids FROM playlists WHERE id = $1 AND owner_id = $2", plID, userID).Scan(&trackIDs)

	var ids []string
	json.Unmarshal(trackIDs, &ids)
	removeSet := make(map[string]bool, len(req.TrackIDs))
	for _, id := range req.TrackIDs {
		removeSet[id] = true
	}
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if !removeSet[id] {
			filtered = append(filtered, id)
		}
	}
	updated, _ := json.Marshal(filtered)

	h.db.ExecContext(r.Context(),
		"UPDATE playlists SET track_ids = $1, updated_at = NOW() WHERE id = $2 AND owner_id = $3",
		updated, plID, userID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// ---- Settings ----

func (h *UserDataHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	rows, err := h.db.QueryContext(r.Context(),
		"SELECT key, value FROM user_settings WHERE user_id = $1", userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		rows.Scan(&k, &v)
		settings[k] = v
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"settings": settings})
}

func (h *UserDataHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	for k, v := range req {
		h.db.ExecContext(r.Context(),
			"INSERT INTO user_settings (user_id, key, value) VALUES ($1, $2, $3) ON CONFLICT (user_id, key) DO UPDATE SET value = $3",
			userID, k, v)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// ---- Queue ----

type queuePayload struct {
	TrackIDs     []string `json:"track_ids"`
	QueueIdx     int      `json:"queue_idx"`
	ShuffleOrder []int    `json:"shuffle_order"`
	ShuffleIdx   int      `json:"shuffle_idx"`
	Mode         string   `json:"mode"`
}

func (h *UserDataHandler) SaveQueue(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req queuePayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	data, _ := json.Marshal(req)
	h.db.ExecContext(r.Context(),
		"INSERT INTO user_settings (user_id, key, value) VALUES ($1, $2, $3) ON CONFLICT (user_id, key) DO UPDATE SET value = $3",
		userID, "player_queue", string(data))

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

type trackSummary struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Artist       string  `json:"artist"`
	Album        string  `json:"album"`
	AlbumID      string  `json:"album_id"`
	Duration     float64 `json:"duration"`
	Suffix       string  `json:"suffix"`
	CoverImageID *string `json:"cover_image_id,omitempty"`
	DiscNumber   int     `json:"disc_number,omitempty"`
	TrackNumber  int     `json:"track,omitempty"`
	Version      int     `json:"version"`
	VersionLabel string  `json:"version_label"`
	ExternalID   string  `json:"external_id"`
}

func (h *UserDataHandler) GetQueue(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// Read stored queue
	var row string
	err := h.db.QueryRowContext(r.Context(),
		"SELECT value FROM user_settings WHERE user_id = $1 AND key = 'player_queue'", userID).Scan(&row)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"queue": nil})
		return
	}

	var q queuePayload
	if err := json.Unmarshal([]byte(row), &q); err != nil || len(q.TrackIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"queue": queuePayload{}, "tracks": []trackSummary{}})
		return
	}

	// Resolve track metadata (maintain order)
	rows, err := h.db.QueryContext(r.Context(),
		`SELECT t.id, t.title, COALESCE(ar.name, ''), COALESCE(sub.album_title, ''), COALESCE(sub.album_id, ''), t.duration, t.file_format, t.cover_image_id,
		 COALESCE(sub.track_number, 0), COALESCE(sub.disc_number, 0), COALESCE(t.version, 0), COALESCE(t.version_label, ''), COALESCE(t.external_id, ''), COALESCE(t.metadata_source, '')
		 FROM tracks t
		 LEFT JOIN track_artists ta ON ta.track_id = t.id AND ta.role = 'performer' AND ta.sort_order = 0
		 LEFT JOIN artists ar ON ta.artist_id = ar.id
		 LEFT JOIN LATERAL (
		     SELECT tal.album_id, al.title AS album_title, tal.track_number, tal.disc_number
		     FROM track_albums tal
		     JOIN albums al ON al.id = tal.album_id
		     WHERE tal.track_id = t.id
		     ORDER BY tal.disc_number, tal.track_number
		     LIMIT 1
		 ) sub ON true
		 WHERE t.id = ANY($1)
		 ORDER BY array_position($1::text[], t.id)`, pq.Array(q.TrackIDs))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	tracks := make([]map[string]interface{}, 0, len(q.TrackIDs))
	trackIDs := make([]string, 0, len(q.TrackIDs))
	for rows.Next() {
		var t trackSummary
		var coverID sql.NullString
		var metaSource string
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.AlbumID, &t.Duration, &t.Suffix, &coverID, &t.TrackNumber, &t.DiscNumber, &t.Version, &t.VersionLabel, &t.ExternalID, &metaSource); err != nil {
			continue
		}
		item := map[string]interface{}{
			"id":              t.ID,
			"title":           t.Title,
			"artist":          t.Artist,
			"duration":        t.Duration,
			"suffix":          t.Suffix,
			"version":         t.Version,
			"version_label":   t.VersionLabel,
			"external_id":     t.ExternalID,
			"metadata_source": metaSource,
		}
		if t.AlbumID != "" {
			item["albums"] = []map[string]interface{}{{"id": t.AlbumID, "title": t.Album, "track": t.TrackNumber, "disc_number": t.DiscNumber}}
		}
		if coverID.Valid {
			item["cover_image_id"] = coverID.String
		}
		tracks = append(tracks, item)
		trackIDs = append(trackIDs, t.ID)
	}

	artistsByTrack := h.loadTrackArtistsBulk(r.Context(), trackIDs)
	for i := range tracks {
		tid := tracks[i]["id"].(string)
		if artists, ok := artistsByTrack[tid]; ok {
			tracks[i]["artists"] = artists
		}
	}

	queueKeys := make(map[repository.VersionGroupKey]struct{})
	for _, t := range tracks {
		if extID, _ := t["external_id"].(string); extID != "" {
			src, _ := t["metadata_source"].(string)
			queueKeys[repository.VersionGroupKey{MetadataSource: src, ExternalID: extID}] = struct{}{}
		}
	}
	if len(queueKeys) > 0 {
		accessibleLibs, _ := h.libraryRepo.FindByUserID(r.Context(), userID)
		var libIDs []string
		for _, l := range accessibleLibs {
			libIDs = append(libIDs, l.ID)
		}
		versionsByExtID, _ := h.trackRepo.FindVersionsByExternalIDBulk(r.Context(), versionKeySlice(queueKeys), libIDs)
		if versionsByExtID != nil {
			for _, t := range tracks {
				extID, _ := t["external_id"].(string)
				if extID == "" {
					continue
				}
				src, _ := t["metadata_source"].(string)
				if siblings, ok := versionsByExtID[repository.VersionGroupKey{MetadataSource: src, ExternalID: extID}]; ok {
					itemID, _ := t["id"].(string)
					if versionList := buildVersionListFor(siblings, src, itemID); len(versionList) > 0 {
						t["versions"] = versionList
					}
				}
			}
		}
	}

	// Adjust queue_idx if current track was deleted
	queueIdx := q.QueueIdx
	if queueIdx >= len(tracks) {
		queueIdx = 0
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tracks":        tracks,
		"queue_idx":     queueIdx,
		"shuffle_order": q.ShuffleOrder,
		"shuffle_idx":   q.ShuffleIdx,
		"mode":          q.Mode,
	})
}

func (h *UserDataHandler) loadTrackArtistsBulk(ctx context.Context, trackIDs []string) map[string][]map[string]interface{} {
	result := make(map[string][]map[string]interface{})
	for _, tid := range trackIDs {
		tas, err := h.trackRepo.LoadTrackArtists(ctx, tid)
		if err != nil || len(tas) == 0 {
			continue
		}
		artists := make([]map[string]interface{}, len(tas))
		for i, ta := range tas {
			entry := map[string]interface{}{
				"artist_id": ta.ArtistID,
				"role":      ta.Role,
			}
			if ta.Artist != nil {
				entry["name"] = ta.Artist.Name
				entry["external_id"] = ta.Artist.ExternalID
			}
			artists[i] = entry
		}
		result[tid] = artists
	}
	return result
}

func (h *UserDataHandler) expandTrackVersions(ctx context.Context, userID string, itemType string, ids []string) []string {
	if itemType != "track" {
		return ids
	}
	accessibleLibs, _ := h.libraryRepo.FindByUserID(ctx, userID)
	var libIDs []string
	for _, l := range accessibleLibs {
		libIDs = append(libIDs, l.ID)
	}
	if len(libIDs) == 0 {
		return ids
	}
	expanded := make([]string, 0, len(ids))
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		var extID, src string
		if err := h.db.QueryRowContext(ctx, "SELECT external_id, metadata_source FROM tracks WHERE id = $1 AND external_id != '' AND library_id = ANY($2)", id, pq.Array(libIDs)).Scan(&extID, &src); err != nil || extID == "" {
			expanded = append(expanded, id)
			seen[id] = true
			continue
		}
		// Version expansion is scoped to the track's metadata source: the
		// group key is (metadata_source, external_id) and a bare external_id
		// could otherwise collide across sources.
		rows, err := h.db.QueryContext(ctx, "SELECT id FROM tracks WHERE external_id = $1 AND metadata_source = $2 AND library_id = ANY($3)", extID, src, pq.Array(libIDs))
		if err != nil {
			expanded = append(expanded, id)
			seen[id] = true
			continue
		}
		for rows.Next() {
			var tid string
			rows.Scan(&tid)
			if !seen[tid] {
				expanded = append(expanded, tid)
				seen[tid] = true
			}
		}
		rows.Close()
	}
	return expanded
}
