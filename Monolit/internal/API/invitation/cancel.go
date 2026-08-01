package invitation

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) CancelCompanyInvitation(w http.ResponseWriter, r *http.Request) {
	h.cancelInvitation(w, r, false)
}

func (h *Handler) CancelDepartmentInvitation(w http.ResponseWriter, r *http.Request) {
	h.cancelInvitation(w, r, true)
}

func (h *Handler) cancelInvitation(w http.ResponseWriter, r *http.Request, withDepartment bool) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid company uuid")
		return
	}

	invitationID, err := uuid.Parse(chi.URLParam(r, "invitation_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid invitation uuid")
		return
	}

	departmentID := uuid.NullUUID{}
	if withDepartment {
		parsedDepartmentID, err := uuid.Parse(chi.URLParam(r, "department_uuid"))
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid department uuid")
			return
		}
		departmentID = uuid.NullUUID{UUID: parsedDepartmentID, Valid: true}
	}

	invitation, err := h.service.CancelInvitation(r.Context(), models.CancelInvitationInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		InvitationUUID: invitationID,
		RequestUser:    requestUserID,
	})
	if err != nil {
		writeInvitationError(w, err, response.CodeFailedToCancelInvitation, "failed to cancel invitation")
		return
	}

	resp, err := converter.InvitationModelToAPI(invitation)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInvitation, "failed to convert invitation")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}
