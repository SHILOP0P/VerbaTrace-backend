package call

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *CallHandler) DeleteCall(w http.ResponseWriter, r *http.Request) {
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

	if err := h.service.DeleteCall(r.Context(), callUUID, userID); err != nil {
		if errors.Is(err, models.ErrCallNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToDeleteCall, "delete call failed")
		return
	}
	response.WriteNoContent(w)
}
