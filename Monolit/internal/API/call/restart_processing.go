package call

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	model "verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type restartProcessingRequest struct {
	// ProcessingMode repeats the field of the upload form on purpose: the choice
	// between a full analysis and a transcript alone is the same choice, and it
	// is made again here because cancelling is usually what precedes changing it.
	ProcessingMode string `json:"processing_mode"`
}

// RestartProcessing starts a cancelled call over, optionally in the cheaper
// mode. Without it a cancelled call was a dead end: the recording was there and
// nothing could be done with it but delete it.
func (h *CallHandler) RestartProcessing(w http.ResponseWriter, r *http.Request) {
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

	var req restartProcessingRequest
	if r.Body != nil {
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil && !errors.Is(decodeErr, io.EOF) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
			return
		}
	}

	mode := strings.TrimSpace(req.ProcessingMode)
	if mode != "" && mode != "analyze" && mode != "transcribe" {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid processing mode")
		return
	}

	restarter, ok := h.service.(interface {
		RestartProcessing(ctx context.Context, id, userID uuid.UUID, transcriptionOnly bool) (model.Call, error)
	})
	if !ok {
		response.WriteError(w, http.StatusNotImplemented, response.CodeFailedToProcessCall, "restarting is not available")
		return
	}

	call, err := restarter.RestartProcessing(r.Context(), callUUID, userID, mode == "transcribe")
	if err != nil {
		switch {
		case errors.Is(err, model.ErrCallNotFound):
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found or cannot be restarted")
		case errors.Is(err, model.ErrTestCallReadOnly):
			response.WriteError(w, http.StatusConflict, response.CodeTestCallReadOnly, "test call is read only")
		case errors.Is(err, model.ErrInvalidCallStatusTransition):
			response.WriteError(w, http.StatusConflict, response.CodeInvalidCallStatus, "call is not waiting to be restarted")
		case errors.Is(err, model.ErrPendingCreditQueueFull):
			response.WriteError(w, http.StatusConflict, response.CodePendingCreditQueueFull, "too many calls are already waiting for credits")
		case errors.Is(err, model.ErrForbidden):
			response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
		default:
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToProcessCall, "failed to restart processing")
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
