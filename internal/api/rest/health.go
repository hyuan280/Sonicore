package rest

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

type HealthHandler struct {
	db *sql.DB
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dbStatus := "up"
	if err := h.db.Ping(); err != nil {
		dbStatus = "down"
	}

	status := "ok"
	if dbStatus == "down" {
		status = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   status,
		"database": dbStatus,
		"version":  "0.1.0",
	})
}
