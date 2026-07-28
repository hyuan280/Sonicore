package rest

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/lib/pq"
	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type UserDataHandler struct {
	db         *sql.DB
	trackRepo  *repository.TrackRepo
	artistRepo *repository.ArtistRepo
	albumRepo  *repository.AlbumRepo
}

func NewUserDataHandler(db *sql.DB) *UserDataHandler {
	return &UserDataHandler{
		db:         db,
		trackRepo:  repository.NewTrackRepo(db),
		artistRepo: repository.NewArtistRepo(db),
		albumRepo:  repository.NewAlbumRepo(db),
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

	var items []map[string]interface{}

	if itemType == "track" {
		rows, err := h.db.QueryContext(r.Context(),
			`SELECT f.item_type, f.item_id, f.created_at,
			        COALESCE(t.title, ''), COALESCE(a.name, ''), COALESCE(al.title, ''),
			        COALESCE(t.album_id, ''), COALESCE(t.duration, 0), COALESCE(t.file_format, ''),
			        t.cover_image_id
			 FROM favorites f
			 LEFT JOIN tracks t ON t.id = f.item_id AND f.item_type = 'track'
			 LEFT JOIN artists a ON a.id = t.artist_id
			 LEFT JOIN albums al ON al.id = t.album_id
			 WHERE f.user_id = $1 AND f.item_type = 'track'
			 ORDER BY f.created_at DESC`, userID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var t, id string
			var ca time.Time
			var title, artist, album, albumID, fileFormat string
			var duration float64
			var coverID sql.NullString
			rows.Scan(&t, &id, &ca, &title, &artist, &album, &albumID, &duration, &fileFormat, &coverID)
			item := map[string]interface{}{
				"item_type": t, "item_id": id, "created_at": ca,
				"title": title, "artist": artist, "album": album,
				"album_id": albumID, "duration": duration, "suffix": fileFormat,
			}
			if coverID.Valid {
				item["cover_image_id"] = coverID.String
			}
			items = append(items, item)
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

	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
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
		h.db.ExecContext(r.Context(),
			"INSERT INTO favorites (user_id, item_type, item_id, created_at) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING",
			userID, req.ItemType, id, now)
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
		h.db.ExecContext(r.Context(),
			"DELETE FROM favorites WHERE user_id = $1 AND item_type = $2 AND item_id = $3",
			userID, req.ItemType, id)
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

	rows, err := h.db.QueryContext(r.Context(),
		`SELECT ph.id, ph.track_id, ph.played_at,
		        COALESCE(t.title, ''), COALESCE(a.name, ''), COALESCE(al.title, ''),
		        COALESCE(t.album_id, ''), COALESCE(t.duration, 0), COALESCE(t.file_format, ''),
		        t.cover_image_id
		 FROM play_history ph
		 INNER JOIN tracks t ON t.id = ph.track_id
		 LEFT JOIN artists a ON a.id = t.artist_id
		 LEFT JOIN albums al ON al.id = t.album_id
		 WHERE ph.user_id = $1
		 ORDER BY ph.played_at DESC LIMIT 100`,
		userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	var items []map[string]interface{}
	for rows.Next() {
		var id, tid string
		var pa time.Time
		var title, artist, album, albumID, fileFormat string
		var duration float64
		var coverID sql.NullString
		rows.Scan(&id, &tid, &pa, &title, &artist, &album,
			&albumID, &duration, &fileFormat, &coverID)
		item := map[string]interface{}{
			"id": id, "track_id": tid, "played_at": pa,
			"title": title, "artist": artist, "album": album,
			"album_id": albumID, "duration": duration, "suffix": fileFormat,
		}
		if coverID.Valid {
			item["cover_image_id"] = coverID.String
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
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
	h.db.ExecContext(r.Context(),
		"INSERT INTO play_history (id, user_id, track_id, played_at) VALUES ($1, $2, $3, $4)",
		domain.NewID(), userID, req.TrackID, now)

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

	var tracks []map[string]interface{}
	for _, tid := range trackIDs {
		track, err := h.trackRepo.FindByID(r.Context(), tid)
		if err != nil {
			continue
		}
		t := map[string]interface{}{
			"id":             track.ID,
			"title":          track.Title,
			"artist_id":      track.ArtistID,
			"album_id":       track.AlbumID,
			"cover_image_id": track.CoverImageID,
			"track":          track.TrackNumber,
			"duration":       track.Duration,
			"bit_rate":       track.BitRate,
			"suffix":         track.FileFormat,
		}
		if a, err := h.artistRepo.FindByID(r.Context(), track.ArtistID); err == nil {
			t["artist"] = a.Name
		}
		if a, err := h.albumRepo.FindByID(r.Context(), track.AlbumID); err == nil {
			t["album"] = a.Title
		}
		tracks = append(tracks, t)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id": plID, "name": name, "tracks": tracks, "created_at": createdAt,
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
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Artist        string  `json:"artist"`
	Album         string  `json:"album"`
	AlbumID       string  `json:"album_id"`
	Duration      float64 `json:"duration"`
	Suffix        string  `json:"suffix"`
	CoverImageID  *string `json:"cover_image_id,omitempty"`
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
		`SELECT t.id, t.title, COALESCE(ar.name, ''), COALESCE(al.title, ''), t.album_id, t.duration, t.file_format, t.cover_image_id
		 FROM tracks t
		 LEFT JOIN artists ar ON t.artist_id = ar.id
		 LEFT JOIN albums al ON t.album_id = al.id
		 WHERE t.id = ANY($1)
		 ORDER BY array_position($1::text[], t.id)`, pq.Array(q.TrackIDs))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
		return
	}
	defer rows.Close()

	tracks := make([]trackSummary, 0, len(q.TrackIDs))
	for rows.Next() {
		var t trackSummary
		var coverID sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.Artist, &t.Album, &t.AlbumID, &t.Duration, &t.Suffix, &coverID); err != nil {
			continue
		}
		if coverID.Valid {
			t.CoverImageID = &coverID.String
		}
		tracks = append(tracks, t)
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
