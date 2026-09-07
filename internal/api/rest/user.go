package rest

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"time"

	_ "golang.org/x/image/webp"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/auth"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type UserHandler struct {
	userRepo     *repository.UserRepo
	sessionStore *cache.SessionStore
	tokenStore   *cache.TokenStore
}

func NewUserHandler(db *sql.DB, sessionStore *cache.SessionStore, tokenStore *cache.TokenStore) *UserHandler {
	return &UserHandler{
		userRepo:     repository.NewUserRepo(db),
		sessionStore: sessionStore,
		tokenStore:   tokenStore,
	}
}

func (h *UserHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrUnauthorized)
		return
	}

	user, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil {
		writeCodedError(w, http.StatusNotFound, domain.ErrUserNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":            user.ID,
		"username":      user.Username,
		"email":         user.Email,
		"role":          user.Role,
		"avatar_format": user.AvatarFormat,
		"created_at":    user.CreatedAt,
	})
}

type updatePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (h *UserHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrUnauthorized)
		return
	}

	var req updatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}

	user, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil {
		writeCodedError(w, http.StatusNotFound, domain.ErrUserNotFound)
		return
	}

	if !auth.CheckPassword(req.OldPassword, user.PasswordHash) {
		writeCodedError(w, http.StatusForbidden, domain.ErrUserWrongPassword)
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrUserHashFailed)
		return
	}

	user.PasswordHash = hash
	user.UpdatedAt = time.Now()
	if err := h.userRepo.Update(r.Context(), user); err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrUserUpdatePassword)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "password updated"})
}

func (h *UserHandler) MeRenew(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrUnauthorized)
		return
	}

	var req struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.SessionToken != "" {
		extErr := h.sessionStore.Extend(r.Context(), req.SessionToken, userID, r.UserAgent())
		if extErr == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if errors.Is(extErr, cache.ErrClientMismatch) {
			h.sessionStore.Revoke(r.Context(), req.SessionToken)
			h.tokenStore.RevokeAll(r.Context(), userID)
			writeCodedError(w, http.StatusUnauthorized, domain.ErrUserClientMismatch)
			return
		}
	}

	sessToken, err := h.sessionStore.Generate(r.Context(), userID, r.UserAgent())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrUserSessionGenerate)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":        "ok",
		"session_token": sessToken,
	})
}

// maxAvatarSize caps stored avatars at 1MB. Uploads are enforced with
// http.MaxBytesReader and re-checked here for safety.
const maxAvatarSize = 1 << 20

// allowedAvatarFormats maps decodable image formats to their MIME types.
var allowedAvatarFormats = map[string]string{
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"webp": "image/webp",
}

// GetAvatar streams the current user's avatar image. 404 means no avatar.
func (h *UserHandler) GetAvatar(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrUnauthorized)
		return
	}

	data, format, err := h.userRepo.GetAvatar(r.Context(), userID)
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

// UploadAvatar stores (or, with an empty body, removes) the current user's
// avatar. Only JPEG/PNG/WebP images up to 1MB are accepted; the uploaded
// bytes are decoded server-side to verify the format.
func (h *UserHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrUnauthorized)
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxAvatarSize+1)
	data, err := io.ReadAll(body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeCodedError(w, http.StatusRequestEntityTooLarge, domain.ErrUserAvatarTooLarge)
		} else {
			writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		}
		return
	}

	// An empty body removes the avatar.
	if len(data) == 0 {
		if err := h.userRepo.UpdateAvatar(r.Context(), userID, nil, ""); err != nil {
			writeCodedError(w, http.StatusInternalServerError, domain.ErrUserAvatarUpdate)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"avatar_format": ""})
		return
	}

	if len(data) > maxAvatarSize {
		writeCodedError(w, http.StatusRequestEntityTooLarge, domain.ErrUserAvatarTooLarge)
		return
	}

	format := sniffAvatarFormat(data)
	if format == "" {
		writeCodedError(w, http.StatusUnsupportedMediaType, domain.ErrUserAvatarFormat)
		return
	}

	if err := h.userRepo.UpdateAvatar(r.Context(), userID, data, format); err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrUserAvatarUpdate)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"avatar_format": format})
}

// sniffAvatarFormat decodes the image header and returns the canonical
// format name when it is an allowed avatar format, "" otherwise.
func sniffAvatarFormat(data []byte) string {
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	if _, ok := allowedAvatarFormats[format]; !ok {
		return ""
	}
	return format
}
