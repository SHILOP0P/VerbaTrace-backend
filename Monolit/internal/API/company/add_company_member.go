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

func (h *Handler) AddCompanyMember(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}

	var req dto.AddCompanyMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	userID, err := uuid.Parse(req.UserUUID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid user uuid")
		return
	}

	member, err := h.service.AddCompanyMember(r.Context(), models.AddCompanyMemberInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRole(req.Role),
	})
	if err != nil {
		if errors.Is(err, models.ErrInvalidCompanyInput) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company input")
			return
		}
		if errors.Is(err, models.ErrCompanyNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
			return
		}
		if errors.Is(err, models.ErrForbidden) {
			response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
			return
		}
		if errors.Is(err, models.ErrSubscriptionRequired) {
			response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
			return
		}
		if errors.Is(err, models.ErrMemberLimitExceeded) {
			response.WriteError(w, http.StatusBadRequest, response.CodeMemberLimitExceeded, "member limit exceeded")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToAddCompanyMember, "failed to add company member")
		return
	}

	resp, err := converter.CompanyMemberModelToAPI(member)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCompany, "failed to convert company member")
		return
	}

	if err := response.WriteJSON(w, http.StatusCreated, resp); err != nil {
		return
	}
}
