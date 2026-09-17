package invitation

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	h.respondToInvitation(w, r, "accept")
}

func (h *Handler) DeclineInvitation(w http.ResponseWriter, r *http.Request) {
	h.respondToInvitation(w, r, "decline")
}

func (h *Handler) respondToInvitation(w http.ResponseWriter, r *http.Request, action string) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	invitationID, err := uuid.Parse(chi.URLParam(r, "invitation_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid invitation uuid")
		return
	}

	var invitation models.MembershipInvitation
	if action == "accept" {
		invitation, err = h.service.AcceptInvitation(r.Context(), models.AcceptInvitationInput{
			InvitationUUID: invitationID,
			RequestUser:    requestUserID,
		})
	} else {
		invitation, err = h.service.DeclineInvitation(r.Context(), models.DeclineInvitationInput{
			InvitationUUID: invitationID,
			RequestUser:    requestUserID,
		})
	}
	if err != nil {
		if action == "accept" {
			writeInvitationError(w, err, response.CodeFailedToAcceptInvitation, "failed to accept invitation")
			return
		}
		writeInvitationError(w, err, response.CodeFailedToDeclineInvitation, "failed to decline invitation")
		return
	}

	resp, err := converter.InvitationModelToAPI(invitation)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInvitation, "failed to convert invitation")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}
