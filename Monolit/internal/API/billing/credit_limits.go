package billing

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

// SetCompanyCreditLimit caps what one company may spend out of the shared pot.
func (h *Handler) SetCompanyCreditLimit(w http.ResponseWriter, r *http.Request) {
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

	var request dto.SetCreditLimitRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	if err := h.service.SetCompanyCreditLimit(r.Context(), models.SetCreditLimitInput{
		CompanyUUID:  companyID,
		UserUUID:     userID,
		LimitCredits: request.LimitCredits,
	}); err != nil {
		writeCreditLimitError(w, err)
		return
	}

	response.WriteNoContent(w)
}

// SetDepartmentCreditLimit splits the company cap between its departments.
func (h *Handler) SetDepartmentCreditLimit(w http.ResponseWriter, r *http.Request) {
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

	var request dto.SetCreditLimitRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	if err := h.service.SetDepartmentCreditLimit(r.Context(), models.SetCreditLimitInput{
		CompanyUUID:    companyID,
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
		UserUUID:       userID,
		LimitCredits:   request.LimitCredits,
	}); err != nil {
		writeCreditLimitError(w, err)
		return
	}

	response.WriteNoContent(w)
}

// GetCompanyCreditForecast shows how the current period is going and where it
// is heading at the present pace.
func (h *Handler) GetCompanyCreditForecast(w http.ResponseWriter, r *http.Request) {
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

	forecast, err := h.service.CompanyCreditForecast(r.Context(), companyID, userID)
	if err != nil {
		writeCreditLimitError(w, err)
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, converter.CompanyCreditForecastModelToAPI(forecast))
}
func writeCreditLimitError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrOwnerOnlyAction):
		response.WriteError(w, http.StatusForbidden, response.CodeOwnerOnlyAction, "Лимит компании задаёт только владелец")
	case errors.Is(err, models.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	case errors.Is(err, models.ErrCompanyNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
	case errors.Is(err, models.ErrInvalidBillingInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid billing input")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetAdminSubscription, "failed to handle credit limit")
	}
}
