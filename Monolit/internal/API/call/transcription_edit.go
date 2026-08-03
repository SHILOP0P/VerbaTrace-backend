package call

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service/transcriptionedit"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *CallHandler) UpdateTranscription(w http.ResponseWriter, r *http.Request) {
	if h.editor == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "transcription editing is not configured")
		return
	}
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return
	}
	var req dto.UpdateTranscriptionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	edits := make([]transcriptionedit.WordEdit, len(req.Edits))
	for i, e := range req.Edits {
		edits[i] = transcriptionedit.WordEdit{WordIndex: e.WordIndex, Text: e.Text, Speaker: e.Speaker}
	}
	transcription, revision, err := h.editor.Update(r.Context(), transcriptionedit.UpdateInput{CallUUID: callID, UserUUID: userID, ExpectedRevision: req.ExpectedRevision, Reason: req.Reason, Edits: edits})
	if err != nil {
		writeTranscriptionEditError(w, err)
		return
	}
	out, err := converter.TranscriptionModelToAPI(transcription)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertTranscription, "failed to convert transcription")
		return
	}
	out.Revision = revision.Revision
	out.Edited = revision.Revision > 1
	out.Editable = true
	result := dto.TranscriptionUpdateResponse{Transcription: out, Revision: revision.Revision, Reason: revision.Reason, ChangedWordIndexes: revision.ChangedWordIndexes}
	_ = response.WriteJSON(w, http.StatusOK, result)
}

func (h *CallHandler) ListTranscriptionRevisions(w http.ResponseWriter, r *http.Request) {
	if h.editor == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "transcription editing is not configured")
		return
	}
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, total, err := h.editor.List(r.Context(), callID, userID, limit, offset)
	if err != nil {
		if errors.Is(err, models.ErrCallNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetTranscription, "failed to list transcription revisions")
		return
	}
	out := make([]dto.TranscriptionRevisionSummary, len(items))
	for i, item := range items {
		out[i] = dto.TranscriptionRevisionSummary{ID: item.ID.String(), CallUUID: item.CallUUID.String(), Revision: item.Revision, Reason: item.Reason, ChangedWordIndexes: item.ChangedWordIndexes, CreatedAt: item.CreatedAt.Format("2006-01-02T15:04:05.999999Z07:00"), IsCurrent: item.IsCurrent}
	}
	_ = response.WriteJSON(w, http.StatusOK, dto.TranscriptionRevisionListResponse{Items: out, Total: total})
}

func (h *CallHandler) GetTranscriptionRevision(w http.ResponseWriter, r *http.Request) {
	if h.editor == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "transcription editing is not configured")
		return
	}
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return
	}
	revision, err := strconv.Atoi(chi.URLParam(r, "revision"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidTranscriptionEdit, "invalid revision")
		return
	}
	content, err := h.editor.GetRevision(r.Context(), callID, userID, revision)
	if err != nil {
		if errors.Is(err, models.ErrTranscriptionNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeFailedToGetTranscription, "transcription revision not found")
			return
		}
		writeTranscriptionEditError(w, err)
		return
	}
	words := make([]dto.TranscriptionWordResponse, len(content.Words))
	for i, word := range content.Words {
		words[i] = dto.TranscriptionWordResponse{Text: word.Text, StartSeconds: word.StartSeconds, EndSeconds: word.EndSeconds, Confidence: word.Confidence, Speaker: word.Speaker}
	}
	segments := make([]dto.TranscriptionSegmentResponse, len(content.Segments))
	for i, segment := range content.Segments {
		segments[i] = dto.TranscriptionSegmentResponse{Speaker: segment.Speaker, StartSeconds: segment.StartSeconds, EndSeconds: segment.EndSeconds, Text: segment.Text}
	}
	_ = response.WriteJSON(w, http.StatusOK, dto.TranscriptionRevisionContentResponse{Revision: content.Revision, IsCurrent: content.IsCurrent, Text: content.Text, Segments: segments, Words: words})
}

func (h *CallHandler) RestoreTranscriptionRevision(w http.ResponseWriter, r *http.Request) {
	if h.editor == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "transcription editing is not configured")
		return
	}
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return
	}
	target, err := strconv.Atoi(chi.URLParam(r, "revision"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidTranscriptionEdit, "invalid revision")
		return
	}
	var req dto.RestoreTranscriptionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	transcription, revision, err := h.editor.Restore(r.Context(), callID, userID, req.ExpectedRevision, target, req.Reason)
	if err != nil {
		writeTranscriptionEditError(w, err)
		return
	}
	out, err := converter.TranscriptionModelToAPI(transcription)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertTranscription, "failed to convert transcription")
		return
	}
	out.Revision, out.Edited, out.Editable = revision.Revision, true, true
	_ = response.WriteJSON(w, http.StatusOK, dto.TranscriptionUpdateResponse{Transcription: out, Revision: revision.Revision, Reason: revision.Reason, ChangedWordIndexes: revision.ChangedWordIndexes})
}

func writeTranscriptionEditError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrInvalidTranscriptionEdit), errors.Is(err, models.ErrNoTranscriptionChanges):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidTranscriptionEdit, "invalid transcription edit")
	case errors.Is(err, models.ErrTranscriptionRevisionConflict):
		response.WriteError(w, http.StatusConflict, response.CodeTranscriptionRevisionConflict, "transcription revision conflict")
	case errors.Is(err, models.ErrTranscriptionEditForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeTranscriptionEditForbidden, "transcription edit forbidden")
	case errors.Is(err, models.ErrTranscriptionNotEditable):
		response.WriteError(w, http.StatusConflict, response.CodeTranscriptionNotEditable, "transcription is not editable")
	case errors.Is(err, models.ErrCallNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToUpdateTranscription, "failed to update transcription")
	}
}
