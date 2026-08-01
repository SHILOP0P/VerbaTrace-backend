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

func (h *Handler) AddDepartmentMember(w http.ResponseWriter, r *http.Request) {
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

	departmentID, err := uuid.Parse(chi.URLParam(r, "department_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid department uuid")
		return
	}

	var req dto.AddDepartmentMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	userID, err := uuid.Parse(req.UserUUID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid user uuid")
		return
	}

	member, err := h.service.AddDepartmentMember(r.Context(), models.AddDepartmentMemberInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    requestUserID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRole(req.Role),
	})
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
		if errors.Is(err, models.ErrMemberLimitExceeded) {
			response.WriteError(w, http.StatusBadRequest, response.CodeMemberLimitExceeded, "member limit exceeded")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToAddDepartmentMember, "failed to add department member")
		return
	}

	resp, err := converter.DepartmentMemberModelToAPI(member)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertDepartment, "failed to convert department member")
		return
	}

	if err := response.WriteJSON(w, http.StatusCreated, resp); err != nil {
		return
	}
}
