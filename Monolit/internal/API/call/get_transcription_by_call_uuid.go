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

func (h *CallHandler) GetTranscriptionByCallUUID(w http.ResponseWriter, r *http.Request) {
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

	transcription, err := h.service.GetTranscriptionByCallUUID(r.Context(), callUUID, userID)
	if err != nil {
		if errors.Is(err, models.ErrCallNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
			return
		}
		if errors.Is(err, models.ErrTranscriptionNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeTranscriptionNotFound, "transcription not found")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetTranscription, "failed to get transcription")
		return
	}

	resp, err := converter.TranscriptionModelToAPI(transcription)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertTranscription, "failed to convert transcription")
		return
	}
	if h.editor != nil {
		if revision, revisionErr := h.editor.CurrentRevision(r.Context(), callUUID, userID); revisionErr == nil {
			resp.Revision = revision
			resp.Edited = revision > 1
			resp.Editable = transcription.Status == models.TranscriptionStatusTranscribed && len(transcription.Words) > 0
		} else if errors.Is(revisionErr, models.ErrCallNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
			return
		}
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
