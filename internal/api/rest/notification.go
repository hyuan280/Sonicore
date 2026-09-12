package rest

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/service"
	"github.com/sonicore/server/internal/infrastructure/logger"
)

type NotificationHandler struct {
	svc *service.NotificationService
}

func NewNotificationHandler(svc *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{svc: svc}
}

func (h *NotificationHandler) ListChannels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"channels": h.svc.Channels(),
	})
}

type setChannelEnabledRequest struct {
	// Pointer so a missing field (empty body) is distinguishable from an
	// explicit false — an empty request must not disable a channel.
	Enabled *bool `json:"enabled"`
}

// SetChannelEnabled enables/disables one channel regardless of its origin
// (built-in email or a plugin-provided channel).
func (h *NotificationHandler) SetChannelEnabled(w http.ResponseWriter, r *http.Request) {
	var req setChannelEnabledRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	ch := domain.ChannelType(mux.Vars(r)["type"])
	if ch == "" || req.Enabled == nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if err := h.svc.SetChannelEnabled(r.Context(), ch, *req.Enabled); err != nil {
		// Unknown channel is the caller's fault, not a server failure.
		if errors.Is(err, service.ErrChannelNotFound) {
			writeCodedError(w, http.StatusNotFound, domain.ErrNotFound)
			return
		}
		logger.Error("[notification] set channel %s enabled=%v failed: %v", ch, req.Enabled, err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type testRequest struct {
	Channel domain.ChannelType `json:"channel"`
	To      []string           `json:"to"`
	Config  map[string]any     `json:"config,omitempty"`
}

func (h *NotificationHandler) SendTest(w http.ResponseWriter, r *http.Request) {
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if req.Channel == "" {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if len(req.To) == 0 {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}

	if err := h.svc.SendTest(r.Context(), req.Channel, req.To, req.Config); err != nil {
		logger.Error("[notification] test send failed: %v", err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrNotificationSendFailed)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "test notification sent"})
}

func (h *NotificationHandler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	prefs := h.svc.GetCategoryPrefs(r.Context())
	writeJSON(w, http.StatusOK, prefs)
}

type updateCategoryPrefsRequest struct {
	Preferences map[domain.NotificationCategory]domain.CategoryPreference `json:"preferences"`
}

func (h *NotificationHandler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	var req updateCategoryPrefsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if err := h.svc.UpdateCategoryPrefs(r.Context(), req.Preferences); err != nil {
		logger.Error("[notification] update category preferences failed: %v", err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrNotificationSavePrefs)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *NotificationHandler) GetUserPrefs(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	prefs, err := h.svc.GetUserPrefs(r.Context(), userID)
	if err != nil {
		logger.Error("[notification] get user prefs failed: %v", err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

type updateUserPrefRequest struct {
	Prefs []domain.UserNotificationPref `json:"prefs"`
}

func (h *NotificationHandler) UpdateUserPref(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	var req updateUserPrefRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, domain.ErrInvalidBody)
		return
	}
	if err := h.svc.UpsertUserPrefs(r.Context(), userID, req.Prefs); err != nil {
		logger.Error("[notification] upsert user prefs failed: %v", err)
		writeCodedError(w, http.StatusInternalServerError, domain.ErrInternal)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
