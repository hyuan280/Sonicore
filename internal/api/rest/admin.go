package rest

import (
	"database/sql"
	"encoding/json"
	"log"
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
	"github.com/sonicore/server/internal/infrastructure/secrets"
)

type AdminHandler struct {
	userRepo     *repository.UserRepo
	settingsRepo *repository.SettingsRepo
	enc          *secrets.Encryptor
}

// NewAdminHandler builds the admin handler. enc encrypts at-rest secrets
// (platform cookies) before they are written to the settings DB; a nil
// Encryptor falls back to plaintext storage.
func NewAdminHandler(db *sql.DB, enc *secrets.Encryptor) *AdminHandler {
	return &AdminHandler{
		userRepo:     repository.NewUserRepo(db),
		settingsRepo: repository.NewSettingsRepo(db),
		enc:          enc,
	}
}

// encryptSecret encrypts a credential when an Encryptor is configured so the
// value at rest in the settings DB is not plaintext. The value to persist is
// returned; callers commit it as part of the settings batch.
func (h *AdminHandler) encryptSecret(plaintext string) (string, error) {
	if plaintext == "" || h.enc == nil {
		return plaintext, nil
	}
	enc, err := h.enc.Encrypt(plaintext)
	if err != nil {
		return "", err
	}
	return enc, nil
}

// cookieBroken reports whether the stored netease cookie exists but cannot be
// decrypted (secret rotation, corruption), mirroring the provider's own
// decrypt-and-degrade behavior in server.go. The failure is logged (without
// the ciphertext) so the divergence is traceable.
func (h *AdminHandler) cookieBroken(raw string) bool {
	if raw == "" || h.enc == nil {
		return false
	}
	if _, err := h.enc.Decrypt(raw); err != nil {
		log.Printf("[admin] netease cookie decrypt failed: %v", err)
		return true
	}
	return false
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
	neEnabled, _ := h.settingsRepo.Get(r.Context(), "metadata_netease_enabled")
	neCookie, _ := h.settingsRepo.Get(r.Context(), "platforms_netease_cookie")
	neRateLimit, _ := h.settingsRepo.Get(r.Context(), "platforms_netease_rate_limit")
	subJukebox, _ := h.settingsRepo.Get(r.Context(), "subsonic_jukebox_id")
	// A stored cookie that no longer decrypts (secret rotation, corruption)
	// is reported so ops can distinguish "not configured" from "configured
	// but unreadable" — the provider silently degrades to anonymous in the
	// latter case.
	cookieBroken := h.cookieBroken(neCookie)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"allow_registration":              allowReg == "true",
		"metadata_musicbrainz_enabled":    mbEnabled == "true",
		"metadata_musicbrainz_api_url":    mbURL,
		"metadata_musicbrainz_rate_limit": mbRateLimit,
		"metadata_netease_enabled":        neEnabled == "true",
		"platforms_netease_rate_limit":    neRateLimit,
		// The cookie itself never leaves the server; only its presence is
		// reported so the credential does not sit in browser state/DOM.
		"platforms_netease_cookie_set":   neCookie != "" && !cookieBroken,
		"platforms_netease_cookie_error": cookieBroken,
		"subsonic_jukebox_id":            subJukebox,
	})
}

type updateSettingsRequest struct {
	AllowRegistration    *bool   `json:"allow_registration,omitempty"`
	MusicBrainzEnabled   *bool   `json:"metadata_musicbrainz_enabled,omitempty"`
	MusicBrainzAPIURL    *string `json:"metadata_musicbrainz_api_url,omitempty"`
	MusicBrainzRateLimit *string `json:"metadata_musicbrainz_rate_limit,omitempty"`
	NeteaseEnabled       *bool   `json:"metadata_netease_enabled,omitempty"`
	NeteaseCookie        *string `json:"platforms_netease_cookie,omitempty"`
	NeteaseCookieClear   *bool   `json:"platforms_netease_cookie_clear,omitempty"`
	NeteaseRateLimit     *string `json:"platforms_netease_rate_limit,omitempty"`
	SubsonicJukeboxID    *string `json:"subsonic_jukebox_id,omitempty"`
}

func (h *AdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	// Validate the whole request before any write, so a rejected batch never
	// leaves a partial state behind (settings written, then a 400).
	if req.NeteaseCookie != nil && req.NeteaseCookieClear != nil &&
		*req.NeteaseCookieClear && *req.NeteaseCookie != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot set and clear the cookie in one request"})
		return
	}

	// Collect every write and commit them in one transaction (SetMany), so a
	// mid-batch write failure rolls the whole update back instead of leaving
	// a partial state.
	writes := make(map[string]string)
	if req.AllowRegistration != nil {
		val := "false"
		if *req.AllowRegistration {
			val = "true"
		}
		writes["allow_registration"] = val
	}
	if req.MusicBrainzEnabled != nil {
		val := "false"
		if *req.MusicBrainzEnabled {
			val = "true"
		}
		writes["metadata_musicbrainz_enabled"] = val
	}
	if req.MusicBrainzAPIURL != nil {
		writes["metadata_musicbrainz_api_url"] = *req.MusicBrainzAPIURL
	}
	if req.MusicBrainzRateLimit != nil {
		writes["metadata_musicbrainz_rate_limit"] = *req.MusicBrainzRateLimit
	}
	if req.NeteaseEnabled != nil {
		val := "false"
		if *req.NeteaseEnabled {
			val = "true"
		}
		writes["metadata_netease_enabled"] = val
	}
	if req.NeteaseCookie != nil {
		// An empty value keeps the existing cookie (the client never holds
		// the raw credential, so it cannot resend it); only a non-empty
		// value overwrites, and a clear is requested explicitly.
		if *req.NeteaseCookie != "" {
			enc, err := h.encryptSecret(*req.NeteaseCookie)
			if err != nil {
				log.Printf("[admin] store platforms_netease_cookie: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store cookie"})
				return
			}
			writes["platforms_netease_cookie"] = enc
		}
	}
	if req.NeteaseCookieClear != nil && *req.NeteaseCookieClear {
		writes["platforms_netease_cookie"] = ""
	}
	if req.NeteaseRateLimit != nil {
		writes["platforms_netease_rate_limit"] = *req.NeteaseRateLimit
	}
	if req.SubsonicJukeboxID != nil {
		writes["subsonic_jukebox_id"] = *req.SubsonicJukeboxID
	}

	if err := h.settingsRepo.SetMany(r.Context(), writes); err != nil {
		log.Printf("[admin] save settings batch: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save settings"})
		return
	}

	mbEnabled, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_enabled")
	mbURL, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_api_url")
	mbRateLimit, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_rate_limit")
	neEnabled, _ := h.settingsRepo.Get(r.Context(), "metadata_netease_enabled")
	neCookie, _ := h.settingsRepo.Get(r.Context(), "platforms_netease_cookie")
	neRateLimit, _ := h.settingsRepo.Get(r.Context(), "platforms_netease_rate_limit")
	allowReg, _ := h.settingsRepo.Get(r.Context(), "allow_registration")
	subJukebox, _ := h.settingsRepo.Get(r.Context(), "subsonic_jukebox_id")
	cookieBroken := h.cookieBroken(neCookie)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"allow_registration":              allowReg == "true",
		"metadata_musicbrainz_enabled":    mbEnabled == "true",
		"metadata_musicbrainz_api_url":    mbURL,
		"metadata_musicbrainz_rate_limit": mbRateLimit,
		"metadata_netease_enabled":        neEnabled == "true",
		"platforms_netease_rate_limit":    neRateLimit,
		"platforms_netease_cookie_set":    neCookie != "" && !cookieBroken,
		"platforms_netease_cookie_error":  cookieBroken,
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
		"current":    browseDir,
		"parent":     parentDir,
		"dirs":       dirs,
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
