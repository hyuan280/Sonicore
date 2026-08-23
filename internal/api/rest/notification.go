package rest

import (
	"encoding/json"
	"net/http"

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
