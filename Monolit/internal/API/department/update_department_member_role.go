package department

import (
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) UpdateDepartmentMemberRole(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, departmentID, userID, ok := departmentMemberRouteParams(w, r)
	if !ok {
		return
	}

	var req dto.UpdateMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	member, err := h.service.UpdateDepartmentMemberRole(r.Context(), models.UpdateDepartmentMemberRoleInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    requestUserID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRole(req.Role),
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

func departmentMemberRouteParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}

	departmentID, err := uuid.Parse(chi.URLParam(r, "department_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid department uuid")
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}

	userID, err := uuid.Parse(chi.URLParam(r, "user_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid user uuid")
		return uuid.Nil, uuid.Nil, uuid.Nil, false
	}

	return companyID, departmentID, userID, true
}

func writeDepartmentMemberError(w http.ResponseWriter, err error, fallbackCode string, fallbackMessage string) {
	if errors.Is(err, models.ErrInvalidDepartmentInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid department input")
		return
	}
	if errors.Is(err, models.ErrCompanyNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
		return
	}
	if errors.Is(err, models.ErrDepartmentNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeDepartmentNotFound, "department member not found")
		return
	}
	if errors.Is(err, models.ErrForbidden) {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
		return
	}
	if errors.Is(err, models.ErrSubscriptionRequired) {
		response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
		return
	}

	response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
}
