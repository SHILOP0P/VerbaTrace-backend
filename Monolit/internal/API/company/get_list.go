package company

import (
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
)

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companies, err := h.service.ListUserCompanies(r.Context(), userID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToListCompanies, "failed to list companies")
		return
	}

	resp := make([]dto.CompanyResponse, len(companies))
	for i, company := range companies {
		companyResponse, err := converter.CompanyModelToAPI(company)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCompany, "failed to convert company")
			return
		}
		resp[i] = companyResponse
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
