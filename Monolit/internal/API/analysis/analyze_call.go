package analysis

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) AnalyzeCall(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	callUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return
	}

	analysis, err := h.service.AnalyzeCall(r.Context(), models.AnalyzeCallInput{
		CallUUID: callUUID,
		UserUUID: userID,
	})
	if err != nil {
		writeAnalyzeError(w, err)
		return
	}

	resp, err := converter.AnalysisModelToAPI(analysis)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToAnalyzeCall, "failed to analyze call")
		return
	}

	if err := response.WriteJSON(w, http.StatusAccepted, resp); err != nil {
		return
	}
}

func writeAnalyzeError(w http.ResponseWriter, err error) {
	if errors.Is(err, models.ErrCallNotFound) || errors.Is(err, models.ErrTranscriptionNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call or transcription not found")
		return
	}
	if errors.Is(err, models.ErrInvalidAnalysisInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInput, "invalid analysis input")
		return
	}
	if errors.Is(err, models.ErrForbidden) {
		response.WriteError(w, http.StatusForbidden, response.CodeCallEditForbidden, "call is read-only for you")
		return
	}
	if errors.Is(err, models.ErrAnalyzerNotConfigured) {
		response.WriteError(w, http.StatusServiceUnavailable, response.CodeAnalyzerNotConfigured, "analyzer not configured")
		return
	}
	if errors.Is(err, models.ErrInvalidAnalysisStatus) {
		response.WriteError(w, http.StatusConflict, response.CodeInvalidAnalysisStatus, "invalid analysis status")
		return
	}
	if errors.Is(err, models.ErrTestCallReadOnly) {
		response.WriteError(w, http.StatusConflict, response.CodeTestCallReadOnly, "test calls cannot be analyzed")
		return
	}
	if errors.Is(err, models.ErrAnalysisRerunForbidden) {
		response.WriteError(w, http.StatusForbidden, response.CodeAnalysisRerunForbidden, "Перезапустить анализ может лидер отдела, заместитель или владелец")
		return
	}

	response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToAnalyzeCall, "failed to analyze call")
}
