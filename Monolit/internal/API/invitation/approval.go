package invitation

import (
	"net/http"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ApproveInvitation releases a leader's invitation for a person the company
// excluded earlier.
func (h *Handler) ApproveInvitation(w http.ResponseWriter, r *http.Request) {
	h.decideInvitation(w, r, true)
}

// RejectInvitation refuses such an invitation.
func (h *Handler) RejectInvitation(w http.ResponseWriter, r *http.Request) {
	h.decideInvitation(w, r, false)
}

func (h *Handler) decideInvitation(w http.ResponseWriter, r *http.Request, approve bool) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}

	invitationID, err := uuid.Parse(chi.URLParam(r, "invitation_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid invitation uuid")
		return
	}

	invitation, err := h.service.DecideInvitationApproval(r.Context(), models.DecideInvitationApprovalInput{
		CompanyUUID:    companyID,
		InvitationUUID: invitationID,
		RequestUser:    requestUserID,
		Approve:        approve,
	})
	if err != nil {
		writeInvitationError(w, err, response.CodeFailedToCancelInvitation, "failed to decide invitation")
		return
	}

	resp, err := converter.InvitationModelToAPI(invitation)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInvitation, "failed to convert invitation")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}

// ListCompanyInvitations shows the owner and the deputy what their company sent
// recently, so nobody spams the same person twice.
func (h *Handler) ListCompanyInvitations(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}

	input := models.ListCompanyInvitationsInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		Status:      models.InvitationStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
	}

	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		since, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid since filter")
			return
		}
		input.Since = &since
	}

	invitations, err := h.service.ListCompanyInvitations(r.Context(), input)
	if err != nil {
		writeInvitationError(w, err, response.CodeFailedToListInvitations, "failed to list company invitations")
		return
	}

	items, err := converter.InvitationsModelToAPI(invitations)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInvitation, "failed to convert invitations")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, items)
}
