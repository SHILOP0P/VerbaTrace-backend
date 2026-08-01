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

func (h *Handler) ListDepartments(w http.ResponseWriter, r *http.Request) {
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

	departments, err := h.service.ListCompanyDepartments(r.Context(), companyID, userID)
	if err != nil {
		if errors.Is(err, models.ErrSubscriptionRequired) {
			response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToListDepartments, "failed to list departments")
		return
	}

	resp := make([]dto.DepartmentResponse, len(departments))
	for i, department := range departments {
		departmentResponse, err := converter.DepartmentModelToAPI(department)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertDepartment, "failed to convert department")
			return
		}
		resp[i] = departmentResponse
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
