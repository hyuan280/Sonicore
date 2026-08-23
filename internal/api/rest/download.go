package rest

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/download"
)

type DownloadHandler struct {
	manager *download.Manager
	perm    *middleware.PermissionChecker
}

func NewDownloadHandler(db *sql.DB, manager *download.Manager) *DownloadHandler {
	return &DownloadHandler{
		manager: manager,
		perm:    middleware.NewPermissionChecker(db),
	}
}

type createDownloadRequest struct {
	URL       string `json:"url"`
	LibraryID string `json:"library_id"`
}

func (h *DownloadHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		writeCodedError(w, http.StatusUnauthorized, domain.ErrUnauthorized)
		return
	}

	var req createDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}

	if req.URL == "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrDownloadURLRequired)
		return
	}

	if req.LibraryID != "" && !h.perm.HasRole(r.Context(), req.LibraryID, userID, middleware.RoleContributor) {
		writeCodedError(w, http.StatusForbidden, domain.ErrLibNeedContributor)
		return
	}

	job, err := h.manager.CreateJob(r.Context(), req.URL, req.LibraryID)
	if err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}

	writeJSON(w, http.StatusAccepted, job)
}

func (h *DownloadHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	libID := mux.Vars(r)["id"]

	if !h.perm.IsMember(r.Context(), libID, userID) {
		writeCodedError(w, http.StatusForbidden, domain.ErrLibAccessDenied)
		return
	}

	jobs, _ := h.manager.List(r.Context(), libID)
	writeJSON(w, http.StatusOK, jobs)
}

func (h *DownloadHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	jobID := mux.Vars(r)["jobId"]

	job, err := h.manager.Get(r.Context(), jobID)
	if err != nil {
		writeCodedError(w, http.StatusNotFound, domain.ErrDownloadJobNotFound)
		return
	}

	if !h.perm.IsMember(r.Context(), job.LibraryID, userID) {
		writeCodedError(w, http.StatusForbidden, domain.ErrLibAccessDenied)
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (h *DownloadHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	jobID := mux.Vars(r)["jobId"]

	job, err := h.manager.Get(r.Context(), jobID)
	if err != nil {
		writeCodedError(w, http.StatusNotFound, domain.ErrDownloadJobNotFound)
		return
	}

	if !h.perm.HasRole(r.Context(), job.LibraryID, userID, middleware.RoleContributor) {
		writeCodedError(w, http.StatusForbidden, domain.ErrLibNeedContributor)
		return
	}

	h.manager.Cancel(jobID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
