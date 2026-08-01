package company

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) GetByUUID(w http.ResponseWriter, r *http.Request) {
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

	company, err := h.service.GetCompanyByUUID(r.Context(), companyID, userID)
	if err != nil {
		if errors.Is(err, models.ErrCompanyNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetCompany, "failed to get company")
		return
	}

	resp, err := converter.CompanyModelToAPI(company)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCompany, "failed to convert company")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
