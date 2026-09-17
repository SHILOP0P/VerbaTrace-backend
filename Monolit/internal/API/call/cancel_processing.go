package call

import (
	"context"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	model "verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// CancelProcessing stops the work on a call. It exists because deleting a call
// mid-flight used to be the only way out, and that left the queue chewing
// through retries on something nobody wanted.
func (h *CallHandler) CancelProcessing(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	callUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call UUID")
		return
	}

	canceller, ok := h.service.(interface {
		CancelProcessing(ctx context.Context, id, userID uuid.UUID) (model.Call, error)
	})
	if !ok {
		response.WriteError(w, http.StatusNotImplemented, response.CodeFailedToProcessCall, "cancelling is not available")
		return
	}

	call, err := canceller.CancelProcessing(r.Context(), callUUID, userID)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrCallNotFound):
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found or no longer being processed")
		case errors.Is(err, model.ErrForbidden):
			response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
		default:
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToProcessCall, "failed to cancel processing")
		}
		return
	}

	apiCall, convertErr := converter.CallModelToAPI(call)
	if convertErr != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCall, "failed to convert call")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, apiCall)
}
