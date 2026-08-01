package company

import (
	"encoding/json"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"
)

func (h *Handler) UpdateCompanyMemberJobTitle(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	companyID, userID, ok := companyMemberRouteParams(w, r)
	if !ok {
		return
	}

	var req dto.UpdateMemberJobTitleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	member, err := h.service.UpdateCompanyMemberJobTitle(r.Context(), models.UpdateCompanyMemberJobTitleInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    userID,
		JobTitle:    req.JobTitle,
	})
	if err != nil {
		writeCompanyMemberError(w, err, response.CodeFailedToUpdateCompanyMember, "failed to update company member")
		return
	}

	result, err := converter.CompanyMemberModelToAPI(member)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCompany, "failed to convert company member")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, result)
}
