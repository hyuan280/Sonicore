package rest

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/task"
)

type TaskHandler struct {
	sched *task.Scheduler
}

func NewTaskHandler(sched *task.Scheduler) *TaskHandler {
	return &TaskHandler{sched: sched}
}

// List returns every registered scheduled task, sorted by next run.
func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.sched.List())
}

// Get returns a single task by ID, or 404 when unknown. This is the
// lightweight status endpoint used by the frontend to poll one task without
// fetching the whole list.
func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	scheduled, err := h.sched.Get(id)
	if errors.Is(err, task.ErrNotFound) {
		writeCodedError(w, http.StatusNotFound, domain.ErrTaskNotFound)
		return
	}
	if err != nil {
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, scheduled)
}

// Run triggers an immediate execution of the task. This is fire-and-forget
// ("trigger") semantics: 202 means the run was accepted, not that it
// succeeded. The actual outcome is observable via the task list (status,
// last_run, last_error).
func (h *TaskHandler) Run(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	err := h.sched.RunNow(id)
	switch {
	case errors.Is(err, task.ErrNotFound):
		writeCodedError(w, http.StatusNotFound, domain.ErrTaskNotFound)
		return
	case errors.Is(err, task.ErrAlreadyRunning):
		writeCodedError(w, http.StatusConflict, domain.ErrTaskAlreadyRunning)
		return
	case err != nil:
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "triggered"})
}

type setTaskEnabledRequest struct {
	// Pointer so a missing field ("{}") is distinguishable from an explicit
	// disable and never silently turns a system task off.
	Enabled *bool `json:"enabled"`
}

// SetEnabled enables or disables the task (persisted across restarts).
func (h *TaskHandler) SetEnabled(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req setTaskEnabledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if req.Enabled == nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}

	if err := h.sched.SetEnabled(id, *req.Enabled); err != nil {
		if errors.Is(err, task.ErrNotFound) {
			writeCodedError(w, http.StatusNotFound, domain.ErrTaskNotFound)
			return
		}
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      id,
		"enabled": *req.Enabled,
	})
}
