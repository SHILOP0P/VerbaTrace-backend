package company

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

func (h *Handler) UpdateCompanyMemberRole(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, userID, ok := companyMemberRouteParams(w, r)
	if !ok {
		return
	}

	var req dto.UpdateMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	member, err := h.service.UpdateCompanyMemberRole(r.Context(), models.UpdateCompanyMemberRoleInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRole(req.Role),
	})
	if err != nil {
		writeCompanyMemberError(w, err, response.CodeFailedToUpdateCompanyMember, "failed to update company member")
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

func companyMemberRouteParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return uuid.Nil, uuid.Nil, false
	}

	userID, err := uuid.Parse(chi.URLParam(r, "user_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid user uuid")
		return uuid.Nil, uuid.Nil, false
	}

	return companyID, userID, true
}

func writeCompanyMemberError(w http.ResponseWriter, err error, fallbackCode string, fallbackMessage string) {
	if errors.Is(err, models.ErrInvalidCompanyInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company input")
		return
	}
	if errors.Is(err, models.ErrCompanyNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company member not found")
		return
	}
	if errors.Is(err, models.ErrForbidden) {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
		return
	}
	if errors.Is(err, models.ErrLastCompanyManager) {
		response.WriteError(w, http.StatusConflict, response.CodeInvalidCompanyInput, "last company manager cannot be removed")
		return
	}
	if errors.Is(err, models.ErrSubscriptionRequired) {
		response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
		return
	}
	if errors.Is(err, models.ErrOwnerOnlyAction) {
		response.WriteError(w, http.StatusForbidden, response.CodeOwnerOnlyAction, "action is available to the company owner only")
		return
	}
	if errors.Is(err, models.ErrCompanyDeputyAlreadyAssigned) {
		response.WriteError(w, http.StatusConflict, response.CodeCompanyDeputyAlreadyAssigned, "company already has a deputy")
		return
	}
	if errors.Is(err, models.ErrOwnershipRecipientBusy) {
		response.WriteError(w, http.StatusConflict, response.CodeOwnershipRecipientBusy, "recipient already owns a company or holds a business plan")
		return
	}
	if errors.Is(err, models.ErrOwnershipScopeMismatch) {
		response.WriteError(w, http.StatusConflict, response.CodeOwnershipScopeMismatch, "the plan covers a different number of companies")
		return
	}
	if errors.Is(err, models.ErrCompanyOwnershipTransferPending) {
		response.WriteError(w, http.StatusConflict, response.CodeOwnershipTransferPending, "company ownership transfer is already pending")
		return
	}
	if errors.Is(err, models.ErrCompanyOwnershipTransferNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeOwnershipTransferNotFound, "company ownership transfer not found")
		return
	}

	response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
}
