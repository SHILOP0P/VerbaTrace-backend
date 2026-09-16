package invitation

import (
	"encoding/json"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) CreateCompanyInvitation(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid company uuid")
		return
	}

	var req dto.CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	userID, ok := parseOptionalUserUUID(w, req.UserUUID)
	if !ok {
		return
	}

	invitation, err := h.service.CreateCompanyInvitation(r.Context(), models.CreateCompanyInvitationInput{
		CompanyUUID:                  companyID,
		RequestUser:                  requestUserID,
		UserUUID:                     userID,
		Username:                     req.Username,
		Role:                         models.CompanyMemberRole(req.Role),
		AcknowledgeCurrentMembership: req.AcknowledgeCurrentMembership,
	})
	if err != nil {
		writeInvitationError(w, err, response.CodeFailedToCreateInvitation, "failed to create invitation")
		return
	}

	resp, err := converter.InvitationModelToAPI(invitation)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInvitation, "failed to convert invitation")
		return
	}

	_ = response.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handler) CreateDepartmentInvitation(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid company uuid")
		return
	}

	departmentID, err := uuid.Parse(chi.URLParam(r, "department_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid department uuid")
		return
	}

	var req dto.CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	userID, ok := parseOptionalUserUUID(w, req.UserUUID)
	if !ok {
		return
	}

	invitation, err := h.service.CreateDepartmentInvitation(r.Context(), models.CreateDepartmentInvitationInput{
		CompanyUUID:                  companyID,
		DepartmentUUID:               departmentID,
		RequestUser:                  requestUserID,
		UserUUID:                     userID,
		Username:                     req.Username,
		Role:                         models.DepartmentMemberRole(req.Role),
		AcknowledgeCurrentMembership: req.AcknowledgeCurrentMembership,
	})
	if err != nil {
		writeInvitationError(w, err, response.CodeFailedToCreateInvitation, "failed to create invitation")
		return
	}

	resp, err := converter.InvitationModelToAPI(invitation)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInvitation, "failed to convert invitation")
		return
	}

	_ = response.WriteJSON(w, http.StatusCreated, resp)
}

func parseOptionalUserUUID(w http.ResponseWriter, raw string) (uuid.UUID, bool) {
	if raw == "" {
		return uuid.Nil, true
	}

	userID, err := uuid.Parse(raw)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid user uuid")
		return uuid.Nil, false
	}

	return userID, true
}
