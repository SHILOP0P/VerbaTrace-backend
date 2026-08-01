package department

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

func (h *Handler) ListDepartmentMembers(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}

	departmentID, err := uuid.Parse(chi.URLParam(r, "department_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid department uuid")
		return
	}

	members, err := h.service.ListDepartmentMembers(r.Context(), companyID, departmentID, userID)
	if err != nil {
		if errors.Is(err, models.ErrInvalidDepartmentInput) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid department input")
			return
		}
		if errors.Is(err, models.ErrCompanyNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
			return
		}
		if errors.Is(err, models.ErrDepartmentNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeDepartmentNotFound, "department not found")
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

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToListDepartmentMembers, "failed to list department members")
		return
	}

	resp := make([]dto.DepartmentMemberResponse, len(members))
	for i, member := range members {
		memberResponse, err := converter.DepartmentMemberModelToAPI(member)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertDepartment, "failed to convert department member")
			return
		}

		resp[i] = memberResponse
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
