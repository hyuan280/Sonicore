package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/internal/infrastructure/secrets"
)

type AdminHandler struct {
	userRepo     *repository.UserRepo
	settingsRepo *repository.SettingsRepo
	enc          *secrets.Encryptor
	reloadNotif  func(ctx context.Context)
}

// NewAdminHandler builds the admin handler. enc encrypts at-rest secrets
// (platform cookies) before they are written to the settings DB; a nil
// Encryptor falls back to plaintext storage. reloadNotif is called after
// settings are saved so the notification service picks up config changes.
func NewAdminHandler(db *sql.DB, enc *secrets.Encryptor, reloadNotif func(ctx context.Context)) *AdminHandler {
	if reloadNotif == nil {
		reloadNotif = func(_ context.Context) {}
	}
	return &AdminHandler{
		userRepo:     repository.NewUserRepo(db),
		settingsRepo: repository.NewSettingsRepo(db),
		enc:          enc,
		reloadNotif:  reloadNotif,
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
		logger.Error("[admin] netease cookie decrypt failed: %v", err)
		return true
	}
	return false
}

// tokenBroken reports whether the stored GitHub token exists but cannot be
// decrypted (secret rotation, corruption). Mirrors cookieBroken.
func (h *AdminHandler) tokenBroken(raw string) bool {
	if raw == "" || h.enc == nil {
		return false
	}
	if _, err := h.enc.Decrypt(raw); err != nil {
		logger.Error("[admin] github token decrypt failed: %v", err)
		return true
	}
	return false
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListAll(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminListUsers)
		return
	}

	result := make([]map[string]interface{}, 0, len(users))
	for _, u := range users {
		result = append(result, map[string]interface{}{
			"id":            u.ID,
			"username":      u.Username,
			"email":         u.Email,
			"role":          u.Role,
			"avatar_format": u.AvatarFormat,
			"created_at":    u.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": result})
}

// GetUserAvatar streams a target user's avatar image. The self-service
// /api/user/avatar endpoint is always scoped to the caller, so admins list
// users through this route. 404 means the user has no avatar.
func (h *AdminHandler) GetUserAvatar(w http.ResponseWriter, r *http.Request) {
	targetID := mux.Vars(r)["id"]
	if targetID == "" {
		http.NotFound(w, r)
		return
	}

	data, format, err := h.userRepo.GetAvatar(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		writeCodedError(w, http.StatusInternalServerError, domain.ErrUserAvatarRead)
		return
	}
	if data == nil || format == "" {
		http.NotFound(w, r)
		return
	}

	contentType := allowedAvatarFormats[format]
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

type updateRoleRequest struct {
	Role string `json:"role"`
}

func (h *AdminHandler) UpdateUserRole(w http.ResponseWriter, r *http.Request) {
	actorID := middleware.GetUserID(r.Context())
	targetID := mux.Vars(r)["id"]

	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}

	newRole := domain.Role(req.Role)
	if newRole != domain.RoleAdmin && newRole != domain.RoleUser {
		writeCodedError(w, http.StatusBadRequest, domain.ErrAdminInvalidRole)
		return
	}

	actor, err := h.userRepo.FindByID(r.Context(), actorID)
	if err != nil {
		writeCodedError(w, http.StatusNotFound, domain.ErrAdminActorNotFound)
		return
	}

	target, err := h.userRepo.FindByID(r.Context(), targetID)
	if err != nil {
		writeCodedError(w, http.StatusNotFound, domain.ErrAdminTargetNotFound)
		return
	}

	if target.Role == domain.RoleSuperAdmin {
		writeCodedError(w, http.StatusForbidden, domain.ErrAdminChangeSuperAdmin)
		return
	}

	if actor.Role == domain.RoleAdmin && target.Role == domain.RoleAdmin {
		writeCodedError(w, http.StatusForbidden, domain.ErrAdminManageAdmin)
		return
	}

	target.Role = newRole
	target.UpdatedAt = time.Now()
	if err := h.userRepo.Update(r.Context(), target); err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminUpdateRole)
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
	logLevel, _ := h.settingsRepo.Get(r.Context(), "log_level")
	githubToken, _ := h.settingsRepo.Get(r.Context(), "plugins_github_token")
	proxyURL, _ := h.settingsRepo.Get(r.Context(), "network_proxy_url")
	githubProxyURL, _ := h.settingsRepo.Get(r.Context(), "network_github_proxy_url")
	githubProxyUseGlobal, _ := h.settingsRepo.Get(r.Context(), "network_github_proxy_use_global")
	// A stored cookie that no longer decrypts (secret rotation, corruption)
	// is reported so ops can distinguish "not configured" from "configured
	// but unreadable" — the provider silently degrades to anonymous in the
	// latter case.
	cookieBroken := h.cookieBroken(neCookie)
	tokenBroken := h.tokenBroken(githubToken)
	resp := map[string]interface{}{
		"allow_registration":              allowReg == "true",
		"metadata_musicbrainz_enabled":    mbEnabled == "true",
		"metadata_musicbrainz_api_url":    mbURL,
		"metadata_musicbrainz_rate_limit": mbRateLimit,
		"metadata_netease_enabled":        neEnabled == "true",
		"platforms_netease_rate_limit":    neRateLimit,
		"platforms_netease_cookie_set":    neCookie != "" && !cookieBroken,
		"platforms_netease_cookie_error":  cookieBroken,
		"subsonic_jukebox_id":             subJukebox,
		"log_level":                       logLevel,
		"plugins_github_token_set":        githubToken != "" && !tokenBroken,
		"plugins_github_token_error":      tokenBroken,
		"network_proxy_url":               proxyURL,
		"network_github_proxy_url":        githubProxyURL,
		"network_github_proxy_use_global": githubProxyUseGlobal == "true",
	}
	h.mergeNotificationSettings(r.Context(), resp)
	writeJSON(w, http.StatusOK, resp)
}

type updateSettingsRequest struct {
	AllowRegistration         *bool   `json:"allow_registration,omitempty"`
	MusicBrainzEnabled        *bool   `json:"metadata_musicbrainz_enabled,omitempty"`
	MusicBrainzAPIURL         *string `json:"metadata_musicbrainz_api_url,omitempty"`
	MusicBrainzRateLimit      *string `json:"metadata_musicbrainz_rate_limit,omitempty"`
	NeteaseEnabled            *bool   `json:"metadata_netease_enabled,omitempty"`
	NeteaseCookie             *string `json:"platforms_netease_cookie,omitempty"`
	NeteaseCookieClear        *bool   `json:"platforms_netease_cookie_clear,omitempty"`
	NeteaseRateLimit          *string `json:"platforms_netease_rate_limit,omitempty"`
	SubsonicJukeboxID         *string `json:"subsonic_jukebox_id,omitempty"`
	LogLevel                  *string `json:"log_level,omitempty"`
	GitHubToken               *string `json:"plugins_github_token,omitempty"`
	GitHubTokenClear          *bool   `json:"plugins_github_token_clear,omitempty"`
	NetworkProxyURL           *string `json:"network_proxy_url,omitempty"`
	NetworkGitHubProxyURL     *string `json:"network_github_proxy_url,omitempty"`
	NetworkGitHubUseGlobal    *bool   `json:"network_github_proxy_use_global,omitempty"`
	NotificationEmailEnabled  *bool   `json:"notification_email_enabled,omitempty"`
	NotificationEmailSMTPHost *string `json:"notification_email_smtp_host,omitempty"`
	NotificationEmailSMTPPort *string `json:"notification_email_smtp_port,omitempty"`
	NotificationEmailUsername *string `json:"notification_email_username,omitempty"`
	NotificationEmailPassword *string `json:"notification_email_password,omitempty"`
	NotificationEmailFromAddr *string `json:"notification_email_from_address,omitempty"`
	NotificationEmailFromName *string `json:"notification_email_from_name,omitempty"`
	NotificationEmailTLS      *bool   `json:"notification_email_tls,omitempty"`
}

func (h *AdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}

	// Validate the whole request before any write, so a rejected batch never
	// leaves a partial state behind (settings written, then a 400).
	if req.NeteaseCookie != nil && req.NeteaseCookieClear != nil &&
		*req.NeteaseCookieClear && *req.NeteaseCookie != "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrAdminCookieConflict)
		return
	}
	if req.GitHubToken != nil && req.GitHubTokenClear != nil &&
		*req.GitHubTokenClear && *req.GitHubToken != "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrAdminTokenConflict)
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
				logger.Info("[admin] store platforms_netease_cookie: %v", err)
				writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminStoreCookie)
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
	if req.LogLevel != nil {
		if _, ok := logger.ParseLevelOk(*req.LogLevel); !ok {
			writeCodedError(w, http.StatusBadRequest, domain.ErrAdminInvalidLogLevel)
			return
		}
		writes["log_level"] = *req.LogLevel
	}
	if req.GitHubToken != nil {
		// An empty value keeps the existing token (the client never holds the
		// raw credential, so it cannot resend it); only a non-empty value
		// overwrites, and a clear is requested explicitly.
		if *req.GitHubToken != "" {
			enc, err := h.encryptSecret(*req.GitHubToken)
			if err != nil {
				logger.Error("[admin] encrypt plugins_github_token: %v", err)
				writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminEncryptSecret)
				return
			}
			writes["plugins_github_token"] = enc
		}
	}
	if req.GitHubTokenClear != nil && *req.GitHubTokenClear {
		writes["plugins_github_token"] = ""
	}
	if req.NetworkProxyURL != nil {
		if !validProxyURL(*req.NetworkProxyURL) {
			writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
			return
		}
		writes["network_proxy_url"] = *req.NetworkProxyURL
	}
	if req.NetworkGitHubProxyURL != nil {
		if !validProxyURL(*req.NetworkGitHubProxyURL) {
			writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
			return
		}
		writes["network_github_proxy_url"] = *req.NetworkGitHubProxyURL
	}
	if req.NetworkGitHubUseGlobal != nil {
		val := "false"
		if *req.NetworkGitHubUseGlobal {
			val = "true"
		}
		writes["network_github_proxy_use_global"] = val
	}
	notifDirty := false
	if req.NotificationEmailEnabled != nil {
		notifDirty = true
		val := "false"
		if *req.NotificationEmailEnabled {
			val = "true"
		}
		writes["notification_email_enabled"] = val
	}
	if req.NotificationEmailSMTPHost != nil {
		notifDirty = true
		writes["notification_email_smtp_host"] = *req.NotificationEmailSMTPHost
	}
	if req.NotificationEmailSMTPPort != nil {
		notifDirty = true
		if port, err := strconv.Atoi(*req.NotificationEmailSMTPPort); err != nil || port < 1 || port > 65535 {
			writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
			return
		}
		writes["notification_email_smtp_port"] = *req.NotificationEmailSMTPPort
	}
	if req.NotificationEmailUsername != nil {
		notifDirty = true
		writes["notification_email_username"] = *req.NotificationEmailUsername
	}
	if req.NotificationEmailPassword != nil {
		notifDirty = true
		if *req.NotificationEmailPassword != "" {
			enc, err := h.encryptSecret(*req.NotificationEmailPassword)
			if err != nil {
				logger.Error("[admin] encrypt notification_email_password: %v", err)
				writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminEncryptSecret)
				return
			}
			writes["notification_email_password"] = enc
		} else {
			writes["notification_email_password"] = ""
		}
	}
	if req.NotificationEmailFromAddr != nil {
		notifDirty = true
		writes["notification_email_from_address"] = *req.NotificationEmailFromAddr
	}
	if req.NotificationEmailFromName != nil {
		notifDirty = true
		writes["notification_email_from_name"] = *req.NotificationEmailFromName
	}
	if req.NotificationEmailTLS != nil {
		notifDirty = true
		val := "false"
		if *req.NotificationEmailTLS {
			val = "true"
		}
		writes["notification_email_tls"] = val
	}

	if err := h.settingsRepo.SetMany(r.Context(), writes); err != nil {
		logger.Info("[admin] save settings batch: %v", err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminSaveSettings)
		return
	}

	if req.LogLevel != nil {
		logger.SetLevel(*req.LogLevel)
	}

	if notifDirty {
		h.reloadNotif(r.Context())
	}

	mbEnabled, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_enabled")
	mbURL, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_api_url")
	mbRateLimit, _ := h.settingsRepo.Get(r.Context(), "metadata_musicbrainz_rate_limit")
	neEnabled, _ := h.settingsRepo.Get(r.Context(), "metadata_netease_enabled")
	neCookie, _ := h.settingsRepo.Get(r.Context(), "platforms_netease_cookie")
	neRateLimit, _ := h.settingsRepo.Get(r.Context(), "platforms_netease_rate_limit")
	allowReg, _ := h.settingsRepo.Get(r.Context(), "allow_registration")
	subJukebox, _ := h.settingsRepo.Get(r.Context(), "subsonic_jukebox_id")
	logLevel, _ := h.settingsRepo.Get(r.Context(), "log_level")
	githubToken, _ := h.settingsRepo.Get(r.Context(), "plugins_github_token")
	proxyURL, _ := h.settingsRepo.Get(r.Context(), "network_proxy_url")
	githubProxyURL, _ := h.settingsRepo.Get(r.Context(), "network_github_proxy_url")
	githubProxyUseGlobal, _ := h.settingsRepo.Get(r.Context(), "network_github_proxy_use_global")
	cookieBroken := h.cookieBroken(neCookie)
	tokenBroken := h.tokenBroken(githubToken)
	resp := map[string]interface{}{
		"allow_registration":              allowReg == "true",
		"metadata_musicbrainz_enabled":    mbEnabled == "true",
		"metadata_musicbrainz_api_url":    mbURL,
		"metadata_musicbrainz_rate_limit": mbRateLimit,
		"metadata_netease_enabled":        neEnabled == "true",
		"platforms_netease_rate_limit":    neRateLimit,
		"platforms_netease_cookie_set":    neCookie != "" && !cookieBroken,
		"platforms_netease_cookie_error":  cookieBroken,
		"subsonic_jukebox_id":             subJukebox,
		"log_level":                       logLevel,
		"plugins_github_token_set":        githubToken != "" && !tokenBroken,
		"plugins_github_token_error":      tokenBroken,
		"network_proxy_url":               proxyURL,
		"network_github_proxy_url":        githubProxyURL,
		"network_github_proxy_use_global": githubProxyUseGlobal == "true",
	}
	h.mergeNotificationSettings(r.Context(), resp)
	writeJSON(w, http.StatusOK, resp)
}

func (h *AdminHandler) mergeNotificationSettings(ctx context.Context, resp map[string]interface{}) {
	keys := []string{
		"notification_email_enabled",
		"notification_email_smtp_host",
		"notification_email_smtp_port",
		"notification_email_username",
		"notification_email_password",
		"notification_email_from_address",
		"notification_email_from_name",
		"notification_email_tls",
	}
	vals, err := h.settingsRepo.GetMany(ctx, keys)
	if err != nil {
		logger.Error("[admin] failed to load notification settings: %v", err)
		return
	}
	resp["notification_email_enabled"] = vals["notification_email_enabled"] == "true"
	resp["notification_email_smtp_host"] = vals["notification_email_smtp_host"]
	resp["notification_email_smtp_port"] = vals["notification_email_smtp_port"]
	resp["notification_email_username"] = vals["notification_email_username"]
	resp["notification_email_password_set"] = vals["notification_email_password"] != ""
	resp["notification_email_from_address"] = vals["notification_email_from_address"]
	resp["notification_email_from_name"] = vals["notification_email_from_name"]
	resp["notification_email_tls"] = vals["notification_email_tls"] == "true"
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
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminReadDirectory)
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

// validProxyURL gates an admin-entered proxy URL. An empty value (meaning "no
// proxy") is allowed; otherwise the value must parse and use a supported proxy
// scheme. The market's proxyFor silently ignores an unparseable URL and
// connects directly, so rejecting bad input here avoids a saved-but-inert
// proxy that is hard to diagnose.
func validProxyURL(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
		return true
	default:
		return false
	}
}

// AdminOnly middleware checks for admin:access permission
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		roleStr := middleware.GetUserRole(r.Context())
		if !port.HasPermission(domain.Role(roleStr), port.PermAdminAccess) {
			writeCodedError(w, http.StatusForbidden, domain.ErrAdminAccessRequired)
			return
		}
		next.ServeHTTP(w, r)
	})
}
