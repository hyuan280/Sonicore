package rest

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/auth"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type AuthHandler struct {
	db            *sql.DB
	jwtService    *auth.JWTService
	userRepo      *repository.UserRepo
	settingsRepo  *repository.SettingsRepo
	tokenStore    *cache.TokenStore
	sessionStore  *cache.SessionStore
	refreshExp    time.Duration
}

func NewAuthHandler(db *sql.DB, jwtService *auth.JWTService, tokenStore *cache.TokenStore, sessionStore *cache.SessionStore, refreshExp time.Duration) *AuthHandler {
	return &AuthHandler{
		db:           db,
		jwtService:   jwtService,
		userRepo:     repository.NewUserRepo(db),
		settingsRepo: repository.NewSettingsRepo(db),
		tokenStore:   tokenStore,
		sessionStore: sessionStore,
		refreshExp:   refreshExp,
	}
}

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token        string      `json:"token"`
	RefreshToken string      `json:"refresh_token"`
	SessionToken string      `json:"session_token"`
	UserID       string      `json:"user_id"`
	Username     string      `json:"username"`
	Role         domain.Role `json:"role"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrAuthRegistrationFields)
		return
	}

	if len(req.Password) < 6 {
		writeCodedError(w, http.StatusBadRequest, domain.ErrAuthPasswordTooShort)
		return
	}

	allowReg, _ := h.settingsRepo.Get(r.Context(), "allow_registration")
	if allowReg != "true" {
		writeCodedError(w, http.StatusForbidden, domain.ErrAuthRegistrationDisabled)
		return
	}

	existing, _ := h.userRepo.FindByUsername(r.Context(), req.Username)
	if existing != nil {
		writeCodedError(w, http.StatusConflict, domain.ErrAuthUsernameExists)
		return
	}

	existing, _ = h.userRepo.FindByEmail(r.Context(), req.Email)
	if existing != nil {
		writeCodedError(w, http.StatusConflict, domain.ErrAuthEmailExists)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAuthHashPassword)
		return
	}

	count, _ := h.userRepo.Count(r.Context())

	now := time.Now()
	role := domain.RoleUser
	if count == 0 {
		role = domain.RoleSuperAdmin
	}
	user := &domain.User{
		ID:           domain.NewID(),
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hash,
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.userRepo.Create(r.Context(), user); err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAuthCreateUser)
		return
	}

	h.writeAuthResponse(w, r, http.StatusCreated, user)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}

	if req.Username == "" || req.Password == "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrAuthCredentialsRequired)
		return
	}

	user, err := h.userRepo.FindByUsername(r.Context(), req.Username)
	if err != nil {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrAuthInvalidCredentials)
		return
	}

	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrAuthInvalidCredentials)
		return
	}

	h.writeAuthResponse(w, r, http.StatusOK, user)
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}

	if req.RefreshToken == "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrAuthRefreshRequired)
		return
	}

	userID, err := h.tokenStore.Validate(r.Context(), req.RefreshToken)
	if err != nil {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrAuthInvalidRefresh)
		return
	}

	if err := h.tokenStore.Revoke(r.Context(), req.RefreshToken); err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAuthRevokeTokens)
		return
	}

	user, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrAuthUserNotFound)
		return
	}

	h.writeAuthResponse(w, r, http.StatusOK, user)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrUnauthorized)
		return
	}

	if err := h.tokenStore.RevokeAll(r.Context(), userID); err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAuthRevokeTokens)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (h *AuthHandler) writeAuthResponse(w http.ResponseWriter, r *http.Request, status int, user *domain.User) {
	accessToken, err := h.jwtService.Generate(user.ID, user.Username, user.Role)
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAuthGenerateToken)
		return
	}

	raw := h.tokenStore.Generate()
	ctx := r.Context()

	if err := h.tokenStore.Store(ctx, user.ID, raw, h.refreshExp); err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAuthStoreToken)
		return
	}

	sessToken, err := h.sessionStore.Generate(ctx, user.ID, r.UserAgent())
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrAuthGenerateSession)
		return
	}

	writeJSON(w, status, authResponse{
		Token:        accessToken,
		RefreshToken: raw,
		SessionToken: sessToken,
		UserID:       user.ID,
		Username:     user.Username,
		Role:         user.Role,
	})
}


