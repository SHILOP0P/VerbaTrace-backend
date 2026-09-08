package call

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var callEventsPollInterval = 2 * time.Second

func (h *CallHandler) Events(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	rawUUID := chi.URLParam(r, "uuid")

	callUUID, err := uuid.Parse(rawUUID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "streaming is not supported")
		return
	}

	call, err := h.service.GetByUUID(r.Context(), callUUID, userID)
	if err != nil {
		if errors.Is(err, models.ErrCallNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToFindCall, "failed to find call")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	lastStatus := call.Status
	if err := writeCallStatusEvent(w, flusher, call); err != nil {
		return
	}
	if isTerminalCallStatus(lastStatus) || (call.TranscriptionOnly && lastStatus == models.CallStatusTranscribed) {
		return
	}

	ticker := time.NewTicker(callEventsPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			call, err := h.service.GetByUUID(r.Context(), callUUID, userID)
			if err != nil {
				_ = writeCallStreamError(w, flusher, "failed_to_find_call")
				return
			}

			if call.Status == lastStatus {
				continue
			}

			lastStatus = call.Status
			if err := writeCallStatusEvent(w, flusher, call); err != nil {
				return
			}
			if isTerminalCallStatus(lastStatus) || (call.TranscriptionOnly && lastStatus == models.CallStatusTranscribed) {
				return
			}
		}
	}
}

func writeCallStatusEvent(w http.ResponseWriter, flusher http.Flusher, call models.Call) error {
	event := dto.CallStatusEvent{
		CallID:    call.ID.String(),
		Status:    string(call.Status),
		Terminal:  isTerminalCallStatus(call.Status) || (call.TranscriptionOnly && call.Status == models.CallStatusTranscribed),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: status\ndata: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()

	return nil
}

func writeCallStreamError(w http.ResponseWriter, flusher http.Flusher, code string) error {
	data, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: error\ndata: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()

	return nil
}

func isTerminalCallStatus(status models.CallStatus) bool {
	return status == models.CallStatusAnalyzed || status == models.CallStatusFailed
}
