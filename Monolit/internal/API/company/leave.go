package company

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) LeaveCompany(w http.ResponseWriter, r *http.Request) {
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
	member, err := h.service.LeaveCompany(r.Context(), companyID, userID)
	if err != nil {
		writeCompanyMemberError(w, err, response.CodeFailedToUpdateCompanyMember, "failed to leave company")
		return
	}
	resp, err := converter.CompanyMemberModelToAPI(member)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCompany, "failed to convert company member")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, resp)
}
