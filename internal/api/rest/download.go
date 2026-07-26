package rest

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/api/middleware"
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
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req createDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
		return
	}

	if req.LibraryID != "" && !h.perm.HasRole(r.Context(), req.LibraryID, userID, middleware.RoleContributor) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "need contributor role or higher"})
		return
	}

	job, err := h.manager.CreateJob(r.Context(), req.URL, req.LibraryID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, job)
}

func (h *DownloadHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	libID := mux.Vars(r)["id"]

	if !h.perm.IsMember(r.Context(), libID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
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
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	if !h.perm.IsMember(r.Context(), job.LibraryID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (h *DownloadHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	jobID := mux.Vars(r)["jobId"]

	job, err := h.manager.Get(r.Context(), jobID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	if !h.perm.HasRole(r.Context(), job.LibraryID, userID, middleware.RoleContributor) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "need contributor role"})
		return
	}

	h.manager.Cancel(jobID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
