package rest

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type AdminHandler struct {
	userRepo     *repository.UserRepo
	settingsRepo *repository.SettingsRepo
}

func NewAdminHandler(db *sql.DB) *AdminHandler {
	return &AdminHandler{
		userRepo:     repository.NewUserRepo(db),
		settingsRepo: repository.NewSettingsRepo(db),
	}
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListAll(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list users"})
		return
	}

	result := make([]map[string]interface{}, 0, len(users))
	for _, u := range users {
		result = append(result, map[string]interface{}{
			"id":         u.ID,
			"username":   u.Username,
			"email":      u.Email,
			"role":       u.Role,
			"created_at": u.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": result})
}

type updateRoleRequest struct {
	Role string `json:"role"`
}

func (h *AdminHandler) UpdateUserRole(w http.ResponseWriter, r *http.Request) {
	actorID := middleware.GetUserID(r.Context())
	targetID := mux.Vars(r)["id"]

	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	newRole := domain.Role(req.Role)
	if newRole != domain.RoleAdmin && newRole != domain.RoleUser {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role, must be admin or user"})
		return
	}

	actor, err := h.userRepo.FindByID(r.Context(), actorID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "actor not found"})
		return
	}

	target, err := h.userRepo.FindByID(r.Context(), targetID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "target user not found"})
		return
	}

	if target.Role == domain.RoleSuperAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot change super admin role"})
		return
	}

	if actor.Role == domain.RoleAdmin && target.Role == domain.RoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admins cannot manage other admins"})
		return
	}

	target.Role = newRole
	target.UpdatedAt = time.Now()
	if err := h.userRepo.Update(r.Context(), target); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update role"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       target.ID,
		"username": target.Username,
		"role":     target.Role,
	})
}

func (h *AdminHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	allowReg, _ := h.settingsRepo.Get(r.Context(), "allow_registration")
	mbEnabled, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_enabled")
	mbURL, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_api_url")
	mbRateLimit, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_rate_limit")
	subJukebox, _ := h.settingsRepo.Get(r.Context(), "subsonic_jukebox_id")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"allow_registration":            allowReg == "true",
		"metadata_musicbrainz_enabled":  mbEnabled == "true",
		"metadata_musicbrainz_api_url":  mbURL,
		"metadata_musicbrainz_rate_limit": mbRateLimit,
		"subsonic_jukebox_id":           subJukebox,
	})
}

type updateSettingsRequest struct {
	AllowRegistration           *bool   `json:"allow_registration,omitempty"`
	MusicBrainzEnabled          *bool   `json:"metadata_musicbrainz_enabled,omitempty"`
	MusicBrainzAPIURL           *string `json:"metadata_musicbrainz_api_url,omitempty"`
	MusicBrainzRateLimit        *string `json:"metadata_musicbrainz_rate_limit,omitempty"`
	SubsonicJukeboxID           *string `json:"subsonic_jukebox_id,omitempty"`
}

func (h *AdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	if req.AllowRegistration != nil {
		val := "false"
		if *req.AllowRegistration {
			val = "true"
		}
		h.settingsRepo.Set(r.Context(), "allow_registration", val)
	}
	if req.MusicBrainzEnabled != nil {
		val := "false"
		if *req.MusicBrainzEnabled {
			val = "true"
		}
		h.settingsRepo.Set(r.Context(), "metadata_musicbrainz_enabled", val)
	}
	if req.MusicBrainzAPIURL != nil {
		h.settingsRepo.Set(r.Context(), "metadata_musicbrainz_api_url", *req.MusicBrainzAPIURL)
	}
	if req.MusicBrainzRateLimit != nil {
		h.settingsRepo.Set(r.Context(), "metadata_musicbrainz_rate_limit", *req.MusicBrainzRateLimit)
	}
	if req.SubsonicJukeboxID != nil {
		h.settingsRepo.Set(r.Context(), "subsonic_jukebox_id", *req.SubsonicJukeboxID)
	}

	mbEnabled, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_enabled")
	mbURL, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_api_url")
	mbRateLimit, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_rate_limit")
	allowReg, _ := h.settingsRepo.Get(r.Context(), "allow_registration")
	subJukebox, _ := h.settingsRepo.Get(r.Context(), "subsonic_jukebox_id")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"allow_registration":              allowReg == "true",
		"metadata_musicbrainz_enabled":    mbEnabled == "true",
		"metadata_musicbrainz_api_url":    mbURL,
		"metadata_musicbrainz_rate_limit": mbRateLimit,
		"subsonic_jukebox_id":             subJukebox,
	})
}

func (h *AdminHandler) ListDirs(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	browseDir := path
	switch {
	case path == "":
		browseDir = "/opt/sonicore/music"
	case !strings.HasSuffix(browseDir, "/"):
		browseDir = filepath.Dir(browseDir)
	}

	entries, err := os.ReadDir(browseDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read directory"})
		return
	}

	var dirs []map[string]string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fullPath := filepath.Join(browseDir, e.Name())
		dirs = append(dirs, map[string]string{
			"name": e.Name(),
			"path": fullPath,
		})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i]["name"] < dirs[j]["name"] })

	parentDir := browseDir
	if filepath.Dir(browseDir) != browseDir {
		parentDir = filepath.Dir(browseDir)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"current":   browseDir,
		"parent":    parentDir,
		"dirs":      dirs,
		"has_parent": browseDir != "/",
	})
}

// AdminOnly middleware checks for admin:access permission
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		roleStr := middleware.GetUserRole(r.Context())
		if !port.HasPermission(domain.Role(roleStr), port.PermAdminAccess) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin access required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
