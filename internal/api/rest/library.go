package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"github.com/lib/pq"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/player"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type LibraryHandler struct {
	db          *sql.DB
	libraryRepo *repository.LibraryRepo
	userRepo    *repository.UserRepo
	perm        *middleware.PermissionChecker
	imagesDir   string
	lyricsDir   string
	covers      *metadata.CoverManager
	manager     *player.EngineManager
}

// NewLibraryHandler builds the library handler. covers is the shared cover
// manager (may be nil; a private one is created then — sharing serializes
// cover mutations against the scanner and cover-handler paths).
func NewLibraryHandler(db *sql.DB, imagesDir, lyricsDir string, covers *metadata.CoverManager, manager *player.EngineManager) *LibraryHandler {
	if covers == nil {
		covers = metadata.NewCoverManager(imagesDir, db, nil)
	}
	return &LibraryHandler{
		db:          db,
		libraryRepo: repository.NewLibraryRepo(db),
		userRepo:    repository.NewUserRepo(db),
		perm:        middleware.NewPermissionChecker(db),
		imagesDir:   imagesDir,
		lyricsDir:   lyricsDir,
		covers:      covers,
		manager:     manager,
	}
}

type createLibraryRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Mode string `json:"metadata_storage_mode"`
}

type addMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

func (h *LibraryHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req createLibraryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = "database"
	} else if mode != "database" && mode != "sidecar" && mode != "both" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "metadata_storage_mode must be database, sidecar, or both"})
		return
	}

	now := time.Now()
	lib := &domain.Library{
		ID:                  domain.NewID(),
		Name:                req.Name,
		Path:                req.Path,
		OwnerID:             userID,
		MetadataStorageMode: mode,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := h.libraryRepo.Create(r.Context(), lib); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create library"})
		return
	}

	member := &domain.LibraryMember{
		LibraryID: lib.ID,
		UserID:    userID,
		Role:      "owner",
		JoinedAt:  now,
	}
	h.libraryRepo.AddMember(r.Context(), member)

	writeJSON(w, http.StatusCreated, lib)
}

func (h *LibraryHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	libraries, err := h.libraryRepo.FindByUserID(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list libraries"})
		return
	}
	if libraries == nil {
		libraries = []domain.Library{}
	}

	writeJSON(w, http.StatusOK, libraries)
}

func (h *LibraryHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	libID := mux.Vars(r)["id"]

	if !h.perm.IsMember(r.Context(), libID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	lib, _ := h.libraryRepo.FindByID(r.Context(), libID)
	writeJSON(w, http.StatusOK, lib)
}

func (h *LibraryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	libID := mux.Vars(r)["id"]

	if !h.perm.IsOwner(r.Context(), libID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the owner can delete a library"})
		return
	}

	h.db.ExecContext(r.Context(),
		`UPDATE jukeboxes SET
			queue = COALESCE(
				(SELECT jsonb_agg(elem) FROM jsonb_array_elements_text(queue) AS elem
				 WHERE elem NOT IN (SELECT id FROM tracks WHERE library_id = $1)),
				'[]'::jsonb
			),
			queue_idx = 0,
			shuffle_order = '[]'::jsonb,
			shuffle_idx = 0`, libID)

	h.db.ExecContext(r.Context(),
		`UPDATE user_settings SET value =
			jsonb_set(value::jsonb, '{track_ids}',
				COALESCE(
					(SELECT jsonb_agg(elem) FROM jsonb_array_elements_text((value::jsonb -> 'track_ids')) AS elem
					 WHERE elem NOT IN (SELECT id FROM tracks WHERE library_id = $1)),
					'[]'::jsonb
				)
			)::text
		WHERE key = 'player_queue'`, libID)

	h.db.ExecContext(r.Context(),
		`UPDATE playlists SET track_ids = (
			SELECT COALESCE(jsonb_agg(elem), '[]'::jsonb)
			FROM jsonb_array_elements_text(track_ids) AS elem
			WHERE elem NOT IN (SELECT id FROM tracks WHERE library_id = $1)
		) WHERE track_ids ?| ARRAY(SELECT id FROM tracks WHERE library_id = $1)`, libID)

	libTrackIDs := map[string]bool{}
	rows, err := h.db.QueryContext(r.Context(), "SELECT id FROM tracks WHERE library_id = $1", libID)
	if err == nil {
		for rows.Next() {
			var tid string
			rows.Scan(&tid)
			libTrackIDs[tid] = true
		}
		rows.Close()
	}

	h.manager.ForEach(func(_ string, eng *player.Engine) {
		status := eng.Status()
		var newQueue []string
		for _, tid := range status.Queue {
			if !libTrackIDs[tid] {
				newQueue = append(newQueue, tid)
			}
		}
		if len(newQueue) != len(status.Queue) {
			if len(newQueue) == 0 {
				eng.ClearQueue()
			} else {
				eng.SetQueue(newQueue)
				if status.Track != nil && libTrackIDs[status.Track.ID] {
					eng.Next()
				}
			}
		}
	})

	if err := h.libraryRepo.Delete(r.Context(), libID); err != nil {
		logger.Error("[library] delete %s failed: %v", libID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete library"})
		return
	}

	// Remove covers of albums that no longer have any track. Only prune the
	// album row when its cover cleanup succeeded; otherwise the images row
	// and files would be orphaned forever (mirrors the scanner path).
	// Favorites are only removed for albums that were actually pruned.
	orphanAlbums := h.orphanAlbumIDs(r.Context())
	var prunedAlbumIDs []string
	for _, id := range orphanAlbums {
		if err := h.covers.DeleteAlbumCovers(r.Context(), id); err != nil {
			logger.Info("[library] delete album covers for %s: %v (album row kept)", id, err)
			continue
		}
		if _, err := h.db.ExecContext(r.Context(), `DELETE FROM albums WHERE id = $1`, id); err != nil {
			logger.Info("[library] delete album row %s: %v (cover already removed)", id, err)
			continue
		}
		prunedAlbumIDs = append(prunedAlbumIDs, id)
	}
	if len(prunedAlbumIDs) > 0 {
		h.db.ExecContext(r.Context(),
			`DELETE FROM favorites WHERE item_type = 'album' AND item_id = ANY($1)`, pq.Array(prunedAlbumIDs))
	}
	h.db.ExecContext(r.Context(),
		`DELETE FROM favorites WHERE item_type = 'artist' AND item_id NOT IN (SELECT DISTINCT artist_id FROM track_artists)`)
	h.db.ExecContext(r.Context(),
		`DELETE FROM artists WHERE id NOT IN (SELECT DISTINCT artist_id FROM track_artists)`)

	if libID != "" {
		os.RemoveAll(filepath.Join(h.imagesDir, libID))
	}
	// Lyrics are stored per library under {data_dir}/lyrics/{library_id}.
	if libID != "" {
		os.RemoveAll(filepath.Join(h.lyricsDir, libID))
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// orphanAlbumIDs returns the ids of albums that are not referenced by any
// track. Called while a library's tracks have already been removed.
func (h *LibraryHandler) orphanAlbumIDs(ctx context.Context) []string {
	rows, err := h.db.QueryContext(ctx,
		`SELECT id FROM albums WHERE id NOT IN (SELECT DISTINCT album_id FROM track_albums)`)
	if err != nil {
		logger.Error("[library] orphan album query error: %v", err)
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			logger.Error("[library] orphan album scan error: %v", err)
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		logger.Error("[library] orphan album iteration error: %v", err)
	}
	return ids
}

func (h *LibraryHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	libID := mux.Vars(r)["id"]

	if !h.perm.IsOwner(r.Context(), libID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the owner can manage members"})
		return
	}

	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Role == "" {
		req.Role = "viewer"
	}
	validRoles := map[string]bool{"admin": true, "contributor": true, "viewer": true}
	if !validRoles[req.Role] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be admin, contributor, or viewer"})
		return
	}

	member := &domain.LibraryMember{
		LibraryID: libID,
		UserID:    req.UserID,
		Role:      req.Role,
		JoinedAt:  time.Now(),
	}

	if err := h.libraryRepo.AddMember(r.Context(), member); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add member"})
		return
	}

	writeJSON(w, http.StatusCreated, member)
}

func (h *LibraryHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	libID := mux.Vars(r)["id"]
	targetID := mux.Vars(r)["userId"]

	if !h.perm.IsOwner(r.Context(), libID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the owner can manage members"})
		return
	}

	lib, _ := h.libraryRepo.FindByID(r.Context(), libID)
	if lib != nil && targetID == lib.OwnerID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot remove the owner"})
		return
	}

	if err := h.libraryRepo.RemoveMember(r.Context(), libID, targetID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to remove member"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (h *LibraryHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	libID := mux.Vars(r)["id"]
	targetID := mux.Vars(r)["userId"]

	if !h.perm.IsOwner(r.Context(), libID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only the owner can manage members"})
		return
	}

	lib, _ := h.libraryRepo.FindByID(r.Context(), libID)
	if lib != nil && targetID == lib.OwnerID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot change the owner's role"})
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	validRoles := map[string]bool{"admin": true, "contributor": true, "viewer": true}
	if !validRoles[req.Role] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be admin, contributor, or viewer"})
		return
	}

	if err := h.libraryRepo.UpdateMemberRole(r.Context(), libID, targetID, req.Role); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update role"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *LibraryHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	libID := mux.Vars(r)["id"]

	if !h.perm.IsMember(r.Context(), libID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	members, err := h.libraryRepo.GetMembers(r.Context(), libID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list members"})
		return
	}
	if members == nil {
		members = []domain.LibraryMember{}
	}

	writeJSON(w, http.StatusOK, members)
}
