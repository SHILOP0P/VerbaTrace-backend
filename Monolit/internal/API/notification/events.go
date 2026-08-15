package notification

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
)

var notificationEventsPollInterval = 2 * time.Second

func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "streaming is not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	seen := make(map[string]struct{})
	emit := func() error {
		result, err := h.service.List(r.Context(), models.ListNotificationsInput{UserUUID: userID, Limit: 50})
		if err != nil {
			return err
		}
		for i := len(result.Notifications) - 1; i >= 0; i-- {
			notification := result.Notifications[i]
			id := notification.ID.String()
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			data, err := json.Marshal(converter.NotificationModelToAPI(notification))
			if err != nil {
				return err
			}
			if _, err = fmt.Fprintf(w, "event: notification\ndata: %s\n\n", data); err != nil {
				return err
			}
			flusher.Flush()
		}
		return nil
	}

	if err := emit(); err != nil {
		return
	}
	ticker := time.NewTicker(notificationEventsPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if err := emit(); err != nil {
				return
			}
		}
	}
}
