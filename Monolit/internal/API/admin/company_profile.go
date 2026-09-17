package admin

import (
	"encoding/json"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
)

// UpdateCompanyTag changes a customer's company tag on their behalf.
//
// It used to bypass everything: no reason, no audit record and no approval from
// the customer, while reading one of their calls demanded all three. The rule is
// the owner's: changing their data and reading their content are the same kind of
// act, and the superadmin is the only exemption from the approval.
func (h *Handler) UpdateCompanyTag(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	companyID, ok := adminCompanyID(w, r)
	if !ok {
		return
	}
	if !h.authorizeCompany(w, r, companyID, "customer_profile") {
		return
	}

	var req dto.UpdateAdminCompanyTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	company, err := h.service.UpdateCompanyTag(r.Context(), models.UpdateAdminCompanyTagInput{
		ActorUserUUID: actor,
		CompanyUUID:   companyID,
		Tag:           req.Tag,
		Metadata:      adminMetadata(r, req.Reason),
	})
	if err != nil {
		writeAdminError(w, err, response.CodeFailedToUpdateCompany, "failed to update company tag")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, adminCompanyResponse(company))
}
