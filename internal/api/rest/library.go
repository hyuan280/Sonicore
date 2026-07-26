package rest

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type LibraryHandler struct {
	db          *sql.DB
	libraryRepo *repository.LibraryRepo
	userRepo    *repository.UserRepo
	perm        *middleware.PermissionChecker
}

func NewLibraryHandler(db *sql.DB) *LibraryHandler {
	return &LibraryHandler{
		db:          db,
		libraryRepo: repository.NewLibraryRepo(db),
		userRepo:    repository.NewUserRepo(db),
		perm:        middleware.NewPermissionChecker(db),
	}
}

type createLibraryRequest struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Mode   string `json:"metadata_storage_mode"`
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

	h.libraryRepo.Delete(r.Context(), libID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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


