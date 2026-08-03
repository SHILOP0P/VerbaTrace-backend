package call

import (
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/service/transcriptionedit"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *CallHandler) ListTranscriptionSpeakerAssignments(w http.ResponseWriter, r *http.Request) {
	callID, userID, ok := h.speakerAssignmentIDs(w, r)
	if !ok {
		return
	}
	items, err := h.editor.ListSpeakerAssignments(r.Context(), callID, userID)
	if err != nil {
		writeTranscriptionEditError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, items)
}

func (h *CallHandler) ReplaceTranscriptionSpeakerAssignments(w http.ResponseWriter, r *http.Request) {
	callID, userID, ok := h.speakerAssignmentIDs(w, r)
	if !ok {
		return
	}
	var items []transcriptionedit.SpeakerAssignment
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&items); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	result, err := h.editor.ReplaceSpeakerAssignments(r.Context(), callID, userID, items)
	if err != nil {
		if errors.Is(err, transcriptionedit.ErrInvalidSpeakerAssignments) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid speaker assignments")
			return
		}
		writeTranscriptionEditError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, result)
}

func (h *CallHandler) speakerAssignmentIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	if h.editor == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "transcription editing is not configured")
		return uuid.Nil, uuid.Nil, false
	}
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return uuid.Nil, uuid.Nil, false
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return uuid.Nil, uuid.Nil, false
	}
	return callID, userID, true
}
