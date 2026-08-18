package rest

import (
	"database/sql"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/service"
)

type ScanHandler struct {
	db      *sql.DB
	scanner *service.ScannerService
	perm    *middleware.PermissionChecker
}

func NewScanHandler(db *sql.DB, scanner *service.ScannerService) *ScanHandler {
	return &ScanHandler{
		db:      db,
		scanner: scanner,
		perm:    middleware.NewPermissionChecker(db),
	}
}

func (h *ScanHandler) Start(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	libID := mux.Vars(r)["id"]

	if !h.perm.HasRole(r.Context(), libID, userID, middleware.RoleContributor) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "need contributor role or higher"})
		return
	}

	mode := r.URL.Query().Get("mode")
	if mode != "overwrite" {
		mode = "missing"
	}

	if err := h.scanner.StartScan(r.Context(), libID, mode); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "scan started", "mode": mode})
}

func (h *ScanHandler) Status(w http.ResponseWriter, r *http.Request) {
	libID := mux.Vars(r)["id"]
	progress := h.scanner.GetProgress(libID)
	if progress == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"library_id": libID,
			"status":     "idle",
		})
		return
	}
	writeJSON(w, http.StatusOK, progress)
}
