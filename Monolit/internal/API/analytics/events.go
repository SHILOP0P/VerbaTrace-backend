package analytics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var deepAnalysisEventsPollInterval = 2 * time.Second

func (h *Handler) DeepAnalysisEvents(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	analysisID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDeepAnalysisInput, "invalid deep analysis uuid")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "streaming is not supported")
		return
	}

	analysis, err := h.service.GetDeepAnalysis(r.Context(), analysisID, userID)
	if err != nil {
		h.writeDeepAnalysisError(w, err, response.CodeFailedToGetDeepAnalysis)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	lastStatus := analysis.Status
	if err := writeDeepAnalysisStatusEvent(w, flusher, analysis); err != nil {
		return
	}
	if isTerminalDeepAnalysisStatus(lastStatus) {
		return
	}

	ticker := time.NewTicker(deepAnalysisEventsPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			analysis, err := h.service.GetDeepAnalysis(r.Context(), analysisID, userID)
			if err != nil {
				_ = writeDeepAnalysisStreamError(w, flusher, "failed_to_get_deep_analysis")
				return
			}
			if analysis.Status == lastStatus {
				continue
			}

			lastStatus = analysis.Status
			if err := writeDeepAnalysisStatusEvent(w, flusher, analysis); err != nil {
				return
			}
			if isTerminalDeepAnalysisStatus(lastStatus) {
				return
			}
		}
	}
}

func writeDeepAnalysisStatusEvent(w http.ResponseWriter, flusher http.Flusher, analysis models.AggregateAnalysis) error {
	event := dto.AggregateAnalysisStatusEvent{
		AnalysisID: analysis.ID.String(),
		Status:     string(analysis.Status),
		Terminal:   isTerminalDeepAnalysisStatus(analysis.Status),
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
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

func writeDeepAnalysisStreamError(w http.ResponseWriter, flusher http.Flusher, code string) error {
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

func isTerminalDeepAnalysisStatus(status models.AggregateAnalysisStatus) bool {
	return status == models.AggregateAnalysisStatusDone || status == models.AggregateAnalysisStatusFailed
}
