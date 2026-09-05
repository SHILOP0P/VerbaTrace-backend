package call

import (
	"errors"
	"net/http"
	"sort"

	"verbatrace/monolit/internal/API/dto"
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
	if h.privacy != nil {
		if state, stateErr := h.privacy.GetCallState(r.Context(), callUUID); stateErr == nil {
			spans, _ := h.privacy.RedactionSpans(r.Context(), callUUID)
			counts := map[string]int{}
			for _, span := range spans {
				counts[span.EntityType]++
				for index := span.WordStartIndex; index <= span.WordEndIndex && index < len(resp.Words); index++ {
					if index >= 0 {
						resp.Words[index].Redaction = &dto.TranscriptionWordRedactionResponse{SpanUUID: span.ID.String(), EntityType: span.EntityType, Label: privacyEntityLabel(span.EntityType), Marker: span.Marker}
					}
				}
			}
			entityCounts := make([]dto.TranscriptionRedactionCount, 0, len(counts))
			for entity, count := range counts {
				entityCounts = append(entityCounts, dto.TranscriptionRedactionCount{EntityType: entity, Label: privacyEntityLabel(entity), Marker: models.PrivacyMarkersRUv1[entity], Count: count})
			}
			sort.Slice(entityCounts, func(i, j int) bool { return entityCounts[i].EntityType < entityCounts[j].EntityType })
			resp.Redaction = &dto.TranscriptionRedactionResponse{Status: state.Status, MarkerContract: state.MarkerContract, SpansCount: len(spans), EntityCounts: entityCounts}
		}
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
