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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username, email, and password are required"})
		return
	}

	if len(req.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 6 characters"})
		return
	}

	allowReg, _ := h.settingsRepo.Get(r.Context(), "allow_registration")
	if allowReg != "true" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "registration is disabled"})
		return
	}

	existing, _ := h.userRepo.FindByUsername(r.Context(), req.Username)
	if existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}

	existing, _ = h.userRepo.FindByEmail(r.Context(), req.Email)
	if existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "email already exists"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
		return
	}

	h.writeAuthResponse(w, r, http.StatusCreated, user)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}

	user, err := h.userRepo.FindByUsername(r.Context(), req.Username)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}

	if !auth.CheckPassword(req.Password, user.PasswordHash) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid username or password"})
		return
	}

	h.writeAuthResponse(w, r, http.StatusOK, user)
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refresh_token is required"})
		return
	}

	userID, err := h.tokenStore.Validate(r.Context(), req.RefreshToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired refresh token"})
		return
	}

	if err := h.tokenStore.Revoke(r.Context(), req.RefreshToken); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke old token"})
		return
	}

	user, err := h.userRepo.FindByID(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "user not found"})
		return
	}

	h.writeAuthResponse(w, r, http.StatusOK, user)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	if err := h.tokenStore.RevokeAll(r.Context(), userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke tokens"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (h *AuthHandler) writeAuthResponse(w http.ResponseWriter, r *http.Request, status int, user *domain.User) {
	accessToken, err := h.jwtService.Generate(user.ID, user.Username, user.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate token"})
		return
	}

	raw := h.tokenStore.Generate()
	ctx := r.Context()

	if err := h.tokenStore.Store(ctx, user.ID, raw, h.refreshExp); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store refresh token"})
		return
	}

	sessToken, _ := h.sessionStore.Generate(ctx, user.ID)

	writeJSON(w, status, authResponse{
		Token:        accessToken,
		RefreshToken: raw,
		SessionToken: sessToken,
		UserID:       user.ID,
		Username:     user.Username,
		Role:         user.Role,
	})
}


