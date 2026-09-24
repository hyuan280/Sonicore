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

// Settings are split by category and served from /api/admin/settings/{category}.
// The request and response bodies are nested objects whose shape mirrors the
// dotted key: the category is the top-level wrapper, then each key segment
// becomes a level (e.g. source.musicbrainz.enabled maps to
// {"source":{"musicbrainz":{"enabled":...}}}). The repo composes the stored
// key from the category and the short name, so handlers only deal in short
// names.

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// settingsCategory resolves and validates the {category} path variable. An
// unknown or empty category is refused so a request can never fall through to
// the wrong section.
func settingsCategory(r *http.Request) (string, bool) {
	switch category := mux.Vars(r)["category"]; category {
	case repository.CategorySystem, repository.CategorySource,
		repository.CategoryNetwork, repository.CategoryNotification:
		return category, true
	default:
		return "", false
	}
}

func (h *AdminHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	category, ok := settingsCategory(r)
	if !ok {
		writeCodedError(w, http.StatusForbidden, domain.ErrAdminSettingsCategory)
		return
	}
	switch category {
	case repository.CategorySystem:
		h.GetSystemSettings(w, r)
	case repository.CategorySource:
		h.GetSourceSettings(w, r)
	case repository.CategoryNetwork:
		h.GetNetworkSettings(w, r)
	case repository.CategoryNotification:
		h.GetNotificationSettings(w, r)
	}
}

func (h *AdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	category, ok := settingsCategory(r)
	if !ok {
		writeCodedError(w, http.StatusForbidden, domain.ErrAdminSettingsCategory)
		return
	}
	switch category {
	case repository.CategorySystem:
		h.UpdateSystemSettings(w, r)
	case repository.CategorySource:
		h.UpdateSourceSettings(w, r)
	case repository.CategoryNetwork:
		h.UpdateNetworkSettings(w, r)
	case repository.CategoryNotification:
		h.UpdateNotificationSettings(w, r)
	}
}

// ---- system ----

type systemSettingsRequest struct {
	System *systemSettingsBody `json:"system"`
}

type systemSettingsBody struct {
	AllowRegistration *bool               `json:"allow_registration"`
	Log               *systemLogBody      `json:"log"`
	Subsonic          *systemSubsonicBody `json:"subsonic"`
	Plugins           *systemPluginsBody  `json:"plugins"`
}

type systemLogBody struct {
	Level *string `json:"level"`
}

type systemSubsonicBody struct {
	JukeboxID *string `json:"jukebox_id"`
}

type systemPluginsBody struct {
	GitHubToken      *string `json:"github_token"`
	GitHubTokenClear *bool   `json:"github_token_clear"`
}

// readSystemSettings loads the system category. A read failure is returned so
// the caller reports 500 instead of 200 with defaults.
func (h *AdminHandler) readSystemSettings(ctx context.Context) (map[string]interface{}, error) {
	vals, err := h.settingsRepo.GetMany(ctx, repository.CategorySystem, []string{
		"allow_registration", "log.level", "subsonic.jukebox_id", "plugins.github_token",
	})
	if err != nil {
		logger.Error("[admin] load system settings: %v", err)
		return nil, err
	}
	githubToken := vals["plugins.github_token"]
	tokenBroken := h.tokenBroken(githubToken)
	return map[string]interface{}{
		"system": map[string]interface{}{
			"allow_registration": vals["allow_registration"] == "true",
			"log": map[string]interface{}{
				"level": vals["log.level"],
			},
			"subsonic": map[string]interface{}{
				"jukebox_id": vals["subsonic.jukebox_id"],
			},
			"plugins": map[string]interface{}{
				"github_token_set":   githubToken != "" && !tokenBroken,
				"github_token_error": tokenBroken,
			},
		},
	}, nil
}

func (h *AdminHandler) GetSystemSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := h.readSystemSettings(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminLoadSettings)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *AdminHandler) UpdateSystemSettings(w http.ResponseWriter, r *http.Request) {
	var req systemSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	writes := make(map[string]string)
	if body := req.System; body != nil {
		if body.AllowRegistration != nil {
			writes["allow_registration"] = boolString(*body.AllowRegistration)
		}
		if body.Log != nil && body.Log.Level != nil {
			if _, ok := logger.ParseLevelOk(*body.Log.Level); !ok {
				writeCodedError(w, http.StatusBadRequest, domain.ErrAdminInvalidLogLevel)
				return
			}
			writes["log.level"] = *body.Log.Level
		}
		if body.Subsonic != nil && body.Subsonic.JukeboxID != nil {
			writes["subsonic.jukebox_id"] = *body.Subsonic.JukeboxID
		}
		if body.Plugins != nil {
			if body.Plugins.GitHubToken != nil && body.Plugins.GitHubTokenClear != nil &&
				*body.Plugins.GitHubTokenClear && *body.Plugins.GitHubToken != "" {
				writeCodedError(w, http.StatusBadRequest, domain.ErrAdminTokenConflict)
				return
			}
			if body.Plugins.GitHubToken != nil && *body.Plugins.GitHubToken != "" {
				enc, err := h.encryptSecret(*body.Plugins.GitHubToken)
				if err != nil {
					logger.Error("[admin] encrypt plugins.github_token: %v", err)
					writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminEncryptSecret)
					return
				}
				writes["plugins.github_token"] = enc
			}
			if body.Plugins.GitHubTokenClear != nil && *body.Plugins.GitHubTokenClear {
				writes["plugins.github_token"] = ""
			}
		}
	}
	if err := h.settingsRepo.SetMany(r.Context(), repository.CategorySystem, writes); err != nil {
		logger.Info("[admin] save system settings: %v", err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminSaveSettings)
		return
	}
	if lvl, ok := writes["log.level"]; ok {
		logger.SetLevel(lvl)
	}
	resp, err := h.readSystemSettings(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminLoadSettings)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- source ----

type sourceSettingsRequest struct {
	Source *sourceSettingsBody `json:"source"`
}

type sourceSettingsBody struct {
	MusicBrainz *sourceMusicBrainzBody `json:"musicbrainz"`
	Netease     *sourceNeteaseBody     `json:"netease"`
}

type sourceMusicBrainzBody struct {
	Enabled   *bool   `json:"enabled"`
	APIURL    *string `json:"api_url"`
	RateLimit *string `json:"rate_limit"`
}

type sourceNeteaseBody struct {
	Enabled     *bool   `json:"enabled"`
	Cookie      *string `json:"cookie"`
	CookieClear *bool   `json:"cookie_clear"`
	RateLimit   *string `json:"rate_limit"`
}

// readSourceSettings loads the source category; a read failure is returned so
// the caller can report 500 instead of 200 with defaults.
func (h *AdminHandler) readSourceSettings(ctx context.Context) (map[string]interface{}, error) {
	vals, err := h.settingsRepo.GetMany(ctx, repository.CategorySource, []string{
		"musicbrainz.enabled", "musicbrainz.api_url", "musicbrainz.rate_limit",
		"netease.enabled", "netease.cookie", "netease.rate_limit",
	})
	if err != nil {
		logger.Error("[admin] load source settings: %v", err)
		return nil, err
	}
	// A stored cookie that no longer decrypts (secret rotation, corruption)
	// is reported so ops can distinguish "not configured" from "configured
	// but unreadable" — the provider silently degrades to anonymous in the
	// latter case.
	cookie := vals["netease.cookie"]
	cookieBroken := h.cookieBroken(cookie)
	return map[string]interface{}{
		"source": map[string]interface{}{
			"musicbrainz": map[string]interface{}{
				"enabled":    vals["musicbrainz.enabled"] == "true",
				"api_url":    vals["musicbrainz.api_url"],
				"rate_limit": vals["musicbrainz.rate_limit"],
			},
			"netease": map[string]interface{}{
				"enabled":      vals["netease.enabled"] == "true",
				"cookie_set":   cookie != "" && !cookieBroken,
				"cookie_error": cookieBroken,
				"rate_limit":   vals["netease.rate_limit"],
			},
		},
	}, nil
}

func (h *AdminHandler) GetSourceSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := h.readSourceSettings(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminLoadSettings)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *AdminHandler) UpdateSourceSettings(w http.ResponseWriter, r *http.Request) {
	var req sourceSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	writes := make(map[string]string)
	if body := req.Source; body != nil {
		if body.MusicBrainz != nil {
			if body.MusicBrainz.Enabled != nil {
				writes["musicbrainz.enabled"] = boolString(*body.MusicBrainz.Enabled)
			}
			if body.MusicBrainz.APIURL != nil {
				writes["musicbrainz.api_url"] = *body.MusicBrainz.APIURL
			}
			if body.MusicBrainz.RateLimit != nil {
				writes["musicbrainz.rate_limit"] = *body.MusicBrainz.RateLimit
			}
		}
		if body.Netease != nil {
			if body.Netease.Cookie != nil && body.Netease.CookieClear != nil &&
				*body.Netease.CookieClear && *body.Netease.Cookie != "" {
				writeCodedError(w, http.StatusBadRequest, domain.ErrAdminCookieConflict)
				return
			}
			if body.Netease.Enabled != nil {
				writes["netease.enabled"] = boolString(*body.Netease.Enabled)
			}
			// An empty value keeps the existing cookie (the client never holds
			// the raw credential, so it cannot resend it); only a non-empty
			// value overwrites, and a clear is requested explicitly.
			if body.Netease.Cookie != nil && *body.Netease.Cookie != "" {
				enc, err := h.encryptSecret(*body.Netease.Cookie)
				if err != nil {
					logger.Info("[admin] store netease.cookie: %v", err)
					writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminStoreCookie)
					return
				}
				writes["netease.cookie"] = enc
			}
			if body.Netease.CookieClear != nil && *body.Netease.CookieClear {
				writes["netease.cookie"] = ""
			}
			if body.Netease.RateLimit != nil {
				writes["netease.rate_limit"] = *body.Netease.RateLimit
			}
		}
	}
	if err := h.settingsRepo.SetMany(r.Context(), repository.CategorySource, writes); err != nil {
		logger.Info("[admin] save source settings: %v", err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminSaveSettings)
		return
	}
	resp, err := h.readSourceSettings(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminLoadSettings)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- network ----

type networkSettingsRequest struct {
	Network *networkSettingsBody `json:"network"`
}

type networkSettingsBody struct {
	ProxyURL *string            `json:"proxy_url"`
	GitHub   *networkGitHubBody `json:"github"`
}

type networkGitHubBody struct {
	ProxyURL  *string `json:"proxy_url"`
	UseGlobal *bool   `json:"use_global"`
}

// readNetworkSettings loads the network category; a read failure is returned
// so the caller can report 500 instead of 200 with defaults.
func (h *AdminHandler) readNetworkSettings(ctx context.Context) (map[string]interface{}, error) {
	vals, err := h.settingsRepo.GetMany(ctx, repository.CategoryNetwork, []string{
		"proxy_url", "github.proxy_url", "github.use_global",
	})
	if err != nil {
		logger.Error("[admin] load network settings: %v", err)
		return nil, err
	}
	return map[string]interface{}{
		"network": map[string]interface{}{
			"proxy_url": vals["proxy_url"],
			"github": map[string]interface{}{
				"proxy_url":  vals["github.proxy_url"],
				"use_global": vals["github.use_global"] == "true",
			},
		},
	}, nil
}

func (h *AdminHandler) GetNetworkSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := h.readNetworkSettings(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminLoadSettings)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *AdminHandler) UpdateNetworkSettings(w http.ResponseWriter, r *http.Request) {
	var req networkSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	writes := make(map[string]string)
	if body := req.Network; body != nil {
		if body.ProxyURL != nil {
			if !validProxyURL(*body.ProxyURL) {
				writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
				return
			}
			writes["proxy_url"] = *body.ProxyURL
		}
		if body.GitHub != nil {
			if body.GitHub.ProxyURL != nil {
				if !validProxyURL(*body.GitHub.ProxyURL) {
					writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
					return
				}
				writes["github.proxy_url"] = *body.GitHub.ProxyURL
			}
			if body.GitHub.UseGlobal != nil {
				writes["github.use_global"] = boolString(*body.GitHub.UseGlobal)
			}
		}
	}
	if err := h.settingsRepo.SetMany(r.Context(), repository.CategoryNetwork, writes); err != nil {
		logger.Info("[admin] save network settings: %v", err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminSaveSettings)
		return
	}
	resp, err := h.readNetworkSettings(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminLoadSettings)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- notification ----

type notificationSettingsRequest struct {
	Notification *notificationSettingsBody `json:"notification"`
}

type notificationSettingsBody struct {
	Email *notificationEmailBody `json:"email"`
}

type notificationEmailBody struct {
	Enabled     *bool   `json:"enabled"`
	SMTPHost    *string `json:"smtp_host"`
	SMTPPort    *string `json:"smtp_port"`
	Username    *string `json:"username"`
	Password    *string `json:"password"`
	FromAddress *string `json:"from_address"`
	FromName    *string `json:"from_name"`
	TLS         *bool   `json:"tls"`
}

// readNotificationSettings loads the notification category; a read failure is
// returned so the caller can report 500 instead of 200 with defaults.
func (h *AdminHandler) readNotificationSettings(ctx context.Context) (map[string]interface{}, error) {
	vals, err := h.settingsRepo.GetMany(ctx, repository.CategoryNotification, []string{
		"email.enabled", "email.smtp_host", "email.smtp_port", "email.username",
		"email.password", "email.from_address", "email.from_name", "email.tls",
	})
	if err != nil {
		logger.Error("[admin] load notification settings: %v", err)
		return nil, err
	}
	return map[string]interface{}{
		"notification": map[string]interface{}{
			"email": map[string]interface{}{
				"enabled":      vals["email.enabled"] == "true",
				"smtp_host":    vals["email.smtp_host"],
				"smtp_port":    vals["email.smtp_port"],
				"username":     vals["email.username"],
				"password_set": vals["email.password"] != "",
				"from_address": vals["email.from_address"],
				"from_name":    vals["email.from_name"],
				"tls":          vals["email.tls"] == "true",
			},
		},
	}, nil
}

func (h *AdminHandler) GetNotificationSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := h.readNotificationSettings(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminLoadSettings)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *AdminHandler) UpdateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	var req notificationSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	writes := make(map[string]string)
	notifDirty := false
	if body := req.Notification; body != nil && body.Email != nil {
		email := body.Email
		if email.Enabled != nil {
			notifDirty = true
			writes["email.enabled"] = boolString(*email.Enabled)
		}
		if email.SMTPHost != nil {
			notifDirty = true
			writes["email.smtp_host"] = *email.SMTPHost
		}
		if email.SMTPPort != nil {
			notifDirty = true
			if port, err := strconv.Atoi(*email.SMTPPort); err != nil || port < 1 || port > 65535 {
				writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
				return
			}
			writes["email.smtp_port"] = *email.SMTPPort
		}
		if email.Username != nil {
			notifDirty = true
			writes["email.username"] = *email.Username
		}
		if email.Password != nil {
			notifDirty = true
			if *email.Password != "" {
				enc, err := h.encryptSecret(*email.Password)
				if err != nil {
					logger.Error("[admin] encrypt notification.email.password: %v", err)
					writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminEncryptSecret)
					return
				}
				writes["email.password"] = enc
			} else {
				writes["email.password"] = ""
			}
		}
		if email.FromAddress != nil {
			notifDirty = true
			writes["email.from_address"] = *email.FromAddress
		}
		if email.FromName != nil {
			notifDirty = true
			writes["email.from_name"] = *email.FromName
		}
		if email.TLS != nil {
			notifDirty = true
			writes["email.tls"] = boolString(*email.TLS)
		}
	}
	if err := h.settingsRepo.SetMany(r.Context(), repository.CategoryNotification, writes); err != nil {
		logger.Info("[admin] save notification settings: %v", err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminSaveSettings)
		return
	}
	if notifDirty {
		h.reloadNotif(r.Context())
	}
	resp, err := h.readNotificationSettings(r.Context())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAdminLoadSettings)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
