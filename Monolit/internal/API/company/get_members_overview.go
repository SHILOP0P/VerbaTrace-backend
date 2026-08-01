package company

import (
	"errors"
	"net/http"
	"strconv"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) GetCompanyMembersOverview(w http.ResponseWriter, r *http.Request) {
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

	input, err := companyMembersInputFromRequest(r, companyID, userID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company input")
		return
	}

	result, err := h.service.ListCompanyMembers(r.Context(), input)
	if err != nil {
		if errors.Is(err, models.ErrInvalidCompanyInput) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company input")
			return
		}
		if errors.Is(err, models.ErrCompanyNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
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

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetCompanyMembers, "failed to get company members")
		return
	}

	resp, err := converter.CompanyMembersResultModelToAPI(result)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCompany, "failed to convert company members")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}

func companyMembersInputFromRequest(r *http.Request, companyID uuid.UUID, userID uuid.UUID) (models.ListCompanyMembersInput, error) {
	query := r.URL.Query()
	input := models.ListCompanyMembersInput{
		CompanyUUID: companyID,
		RequestUser: userID,
		Query:       query.Get("q"),
		Limit:       20,
		Offset:      0,
	}

	if value := query.Get("status"); value != "" {
		status := models.MembershipStatus(value)
		input.Status = &status
	}

	if value := query.Get("role"); value != "" {
		role := value
		input.Role = &role
	}

	if value := query.Get("department_uuid"); value != "" {
		departmentID, err := uuid.Parse(value)
		if err != nil {
			return models.ListCompanyMembersInput{}, err
		}
		input.DepartmentUUID = departmentID
	}

	if value := query.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil {
			return models.ListCompanyMembersInput{}, err
		}
		input.Limit = limit
	}

	if value := query.Get("offset"); value != "" {
		offset, err := strconv.Atoi(value)
		if err != nil {
			return models.ListCompanyMembersInput{}, err
		}
		input.Offset = offset
	}

	return input, nil
}
