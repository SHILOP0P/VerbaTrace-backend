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

func (h *Handler) GetByCallUUID(w http.ResponseWriter, r *http.Request) {
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

	analysis, err := h.service.GetByCallUUID(r.Context(), callUUID, userID)
	if err != nil {
		writeGetAnalysisError(w, err)
		return
	}

	resp, err := converter.AnalysisModelToAPI(analysis)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetAnalysis, "failed to get analysis")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}

func writeGetAnalysisError(w http.ResponseWriter, err error) {
	if errors.Is(err, models.ErrCallNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
		return
	}
	if errors.Is(err, models.ErrAnalysisNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeAnalysisNotFound, "analysis not found")
		return
	}
	if errors.Is(err, models.ErrInvalidAnalysisInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInput, "invalid analysis input")
		return
	}

	response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetAnalysis, "failed to get analysis")
}
