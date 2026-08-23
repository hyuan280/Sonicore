package rest

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

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
		"id":         user.ID,
		"username":   user.Username,
		"email":      user.Email,
		"role":       user.Role,
		"created_at": user.CreatedAt,
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
