package department

import (
	"encoding/json"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"
)

func (h *Handler) UpdateDepartmentMemberStatus(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, departmentID, userID, ok := departmentMemberRouteParams(w, r)
	if !ok {
		return
	}

	var req dto.UpdateMemberStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	member, err := h.service.UpdateDepartmentMemberStatus(r.Context(), models.UpdateDepartmentMemberStatusInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    requestUserID,
		UserUUID:       userID,
		Status:         models.MembershipStatus(req.Status),
	})
	if err != nil {
		writeDepartmentMemberError(w, err, response.CodeFailedToUpdateDepartmentMember, "failed to update department member")
		return
	}

	resp, err := converter.DepartmentMemberModelToAPI(member)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertDepartment, "failed to convert department member")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
