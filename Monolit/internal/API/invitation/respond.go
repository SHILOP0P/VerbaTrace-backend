package invitation

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

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
			InvitationUUID:  invitationID,
			RequestUser:     requestUserID,
			ConfirmTransfer: confirmTransferRequested(r),
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

// confirmTransferRequested reads the answer to the "you are leaving your current
// company" dialog. It is accepted both in the body and in the query so the
// confirmation survives a plain retry of the same request.
func confirmTransferRequested(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("confirm_transfer")), "true") {
		return true
	}

	if r.Body == nil {
		return false
	}

	var body struct {
		ConfirmTransfer bool `json:"confirm_transfer"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil {
		return false
	}

	return body.ConfirmTransfer
}
