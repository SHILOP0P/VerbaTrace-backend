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

func (h *Handler) CreateDepartment(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	rawUUID := chi.URLParam(r, "uuid")
	companyID, err := uuid.Parse(rawUUID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}

	var req dto.CreateDepartmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	department, err := h.service.CreateDepartment(r.Context(), models.CreateDepartmentInput{
		CompanyUUID: companyID,
		UserID:      userID,
		Name:        req.Name,
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
		if errors.Is(err, models.ErrForbidden) {
			response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
			return
		}
		if errors.Is(err, models.ErrSubscriptionRequired) {
			response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
			return
		}
		if errors.Is(err, models.ErrDepartmentLimitExceeded) {
			response.WriteError(w, http.StatusBadRequest, response.CodeDepartmentLimitExceeded, "department limit exceeded")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToCreateDepartment, "failed to create department")
		return
	}

	resp, err := converter.DepartmentModelToAPI(department)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertDepartment, "failed to convert department")
		return
	}

	if err := response.WriteJSON(w, http.StatusCreated, resp); err != nil {
		return
	}
}
