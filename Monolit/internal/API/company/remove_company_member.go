package company

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"
)

type removeCompanyMemberRequest struct {
	Reason string `json:"reason"`
}

// RemoveCompanyMember excludes a member from the company. Membership is never
// deleted, the row keeps the history with the "left" status.
func (h *Handler) RemoveCompanyMember(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, userID, ok := companyMemberRouteParams(w, r)
	if !ok {
		return
	}

	var req removeCompanyMemberRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
			return
		}
	}

	member, err := h.service.RemoveCompanyMember(r.Context(), models.RemoveCompanyMemberInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    userID,
		Reason:      strings.TrimSpace(req.Reason),
	})
	if err != nil {
		writeCompanyMemberError(w, err, response.CodeFailedToUpdateCompanyMember, "failed to remove company member")
		return
	}

	resp, err := converter.CompanyMemberModelToAPI(member)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCompany, "failed to convert company member")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
