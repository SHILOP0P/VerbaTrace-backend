package call

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
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
	h.enrichCallPrivacy(r, call, userID, &resp)
	h.enrichCallAccess(r, call.ID, userID, &resp)

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}

func (h *CallHandler) enrichCallPrivacy(r *http.Request, call models.Call, userID uuid.UUID, resp *dto.CallResponse) {
	if h.privacy != nil {
		state, stateErr := h.privacy.EnsureCallState(r.Context(), call)
		if stateErr == nil {
			capabilities, capErr := h.privacy.Capabilities(r.Context(), call, userID)
			if capErr == nil {
				mediaStatus := "not_requested"
				if media, mediaErr := h.privacy.GetSanitizedMedia(r.Context(), call.ID); mediaErr == nil {
					mediaStatus = media.Status
				}
				policyVersion := 0
				if state.PolicyVersionID.Valid {
					policyVersion, _ = h.privacy.PolicyVersionNumber(r.Context(), state.PolicyVersionID.UUID)
				}
				label := map[string]string{"personal": "Личная политика", "company": "Политика компании", "none": "Не настроено", "simulated": "Тестовый режим"}[state.PolicySource]
				recommended := "original"
				if !capabilities.CanReadOriginalMedia {
					recommended = "redacted"
				}
				resp.Privacy = &dto.CallPrivacyResponse{Status: state.Status, Protected: state.Status == "ready" && state.PolicySnapshot.Enabled, MarkerContract: state.MarkerContract, PolicyVersion: policyVersion, PolicySourceLabel: label, DetectedSpans: state.DetectedSpans, RecommendedMediaVariant: recommended, SanitizedMediaStatus: mediaStatus, Capabilities: capabilities}
			}
		}
	}
}
