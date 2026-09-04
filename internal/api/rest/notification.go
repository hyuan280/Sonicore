package rest

import (
	"encoding/json"
	"net/http"

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
