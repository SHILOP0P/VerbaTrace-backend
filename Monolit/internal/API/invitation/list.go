package invitation

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"
)

func (h *Handler) ListUserInvitations(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	invitations, err := h.service.ListUserInvitations(r.Context(), models.ListUserInvitationsInput{
		UserUUID: userID,
		Status:   models.InvitationStatus(r.URL.Query().Get("status")),
	})
	if err != nil {
		writeInvitationError(w, err, response.CodeFailedToListInvitations, "failed to list invitations")
		return
	}

	resp, err := converter.InvitationsModelToAPI(invitations)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInvitation, "failed to convert invitation")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}
