package call

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *CallHandler) GetByUUID(w http.ResponseWriter, r *http.Request) {
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

	call, err := h.service.GetByUUID(r.Context(), callUUID, userID)
	if err != nil {
		if errors.Is(err, models.ErrCallNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToFindCall, "failed to find call")
		return
	}

	resp, err := converter.CallModelToAPI(call)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCall, "failed to convert call")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
