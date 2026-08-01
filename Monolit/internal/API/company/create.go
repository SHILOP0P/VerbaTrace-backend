package company

import (
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"
)

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	var req dto.CreateCompanyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	company, err := h.service.CreateCompany(r.Context(), models.CreateCompanyInput{
		Name:          req.Name,
		ManagerUserID: userID,
	})
	if err != nil {
		if errors.Is(err, models.ErrInvalidCompanyInput) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company input")
			return
		}
		if errors.Is(err, models.ErrUserAlreadyManagesCompany) {
			response.WriteError(w, http.StatusConflict, response.CodeUserAlreadyManagesCompany, "user already manages company")
			return
		}
		if errors.Is(err, models.ErrSubscriptionRequired) {
			response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
			return
		}
		if errors.Is(err, models.ErrCompanyLimitExceeded) {
			response.WriteError(w, http.StatusBadRequest, response.CodeCompanyLimitExceeded, "company limit exceeded")
			return
		}
		if errors.Is(err, models.ErrPlanLimitExceeded) {
			response.WriteError(w, http.StatusBadRequest, response.CodePlanLimitExceeded, "plan limit exceeded")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToCreateCompany, "failed to create company")
		return
	}

	resp, err := converter.CompanyModelToAPI(company)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCompany, "failed to convert company")
		return
	}

	if err := response.WriteJSON(w, http.StatusCreated, resp); err != nil {
		return
	}
}
