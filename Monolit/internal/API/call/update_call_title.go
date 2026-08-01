package call

import (
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *CallHandler) UpdateCallTitle(w http.ResponseWriter, r *http.Request) {
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

	var req dto.UpdateCallTitleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	updatedCall, err := h.service.UpdateCallTitle(r.Context(), callUUID, userID, req.Title)
	if err != nil {
		if errors.Is(err, models.ErrInvalidCallTitle) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallTitle, "invalid call title")
			return
		}
		if errors.Is(err, models.ErrCallNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToUpdateCallTitle, "failed to update call title")
		return
	}

	resp, err := converter.CallModelToAPI(updatedCall)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCall, "failed to convert call")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}

}
