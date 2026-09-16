package invitation

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service"

	"github.com/google/uuid"
)

type Handler struct {
	service service.InvitationService
}

func NewHandler(service service.InvitationService) *Handler {
	return &Handler{service: service}
}

func userIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return middleware.UserIDFromContext(r.Context())
}

func writeInvitationError(w http.ResponseWriter, err error, fallbackCode string, fallbackMessage string) {
	if errors.Is(err, models.ErrInvalidInvitationInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInvitationInput, "invalid invitation input")
		return
	}
	if errors.Is(err, models.ErrInvalidCompanyInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company input")
		return
	}
	if errors.Is(err, models.ErrInvalidDepartmentInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid department input")
		return
	}
	if errors.Is(err, models.ErrInvitationAlreadyExists) {
		response.WriteError(w, http.StatusConflict, response.CodeInvitationAlreadyExists, "invitation already exists")
		return
	}
	if errors.Is(err, models.ErrInvitationNotPending) {
		response.WriteError(w, http.StatusConflict, response.CodeInvitationNotPending, "invitation not pending")
		return
	}
	if errors.Is(err, models.ErrInvitationExpired) {
		response.WriteError(w, http.StatusConflict, response.CodeInvitationExpired, "invitation expired")
		return
	}
	if errors.Is(err, models.ErrInvitationNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeInvitationNotFound, "invitation not found")
		return
	}
	if errors.Is(err, models.ErrCompanyNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
		return
	}
	if errors.Is(err, models.ErrDepartmentNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeDepartmentNotFound, "department not found")
		return
	}
	if errors.Is(err, models.ErrUserNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeUserNotFound, "user not found")
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
	// The interface turns these three into confirmation dialogs, so they carry
	// enough context to name the company the user is about to leave.
	var conflict *models.CompanyMembershipConflict
	if errors.As(err, &conflict) {
		response.WriteErrorWithDetails(w, http.StatusConflict, response.CodeCompanyMembershipConflict, "user already belongs to another company", map[string]any{
			"current_company_uuid": conflict.CurrentCompanyUUID,
			"current_company_name": conflict.CurrentCompanyName,
			"confirmation_field":   "confirm_transfer",
		})
		return
	}
	if errors.Is(err, models.ErrCompanyMembershipConflict) {
		response.WriteError(w, http.StatusConflict, response.CodeCompanyMembershipConflict, "user already belongs to another company")
		return
	}
	var transferRequired *models.DepartmentTransferRequired
	if errors.As(err, &transferRequired) {
		response.WriteErrorWithDetails(w, http.StatusConflict, response.CodeDepartmentTransferRequired, "department transfer request is required", map[string]any{
			"user_uuid": transferRequired.UserUUID,
		})
		return
	}
	if errors.Is(err, models.ErrTargetAlreadyEngaged) {
		response.WriteErrorWithDetails(w, http.StatusConflict, response.CodeTargetAlreadyEngaged, "user already belongs to a company or department", map[string]any{
			"confirmation_field": "acknowledge_current_membership",
		})
		return
	}
	if errors.Is(err, models.ErrInvitationsMuted) {
		response.WriteError(w, http.StatusConflict, response.CodeInvitationsMuted, "user does not accept invitations")
		return
	}
	if errors.Is(err, models.ErrInvitationApprovalRequired) {
		response.WriteError(w, http.StatusConflict, response.CodeInvitationApprovalRequired, "invitation needs approval")
		return
	}
	if errors.Is(err, models.ErrOwnerOnlyAction) {
		response.WriteError(w, http.StatusForbidden, response.CodeOwnerOnlyAction, "action is available to the company owner only")
		return
	}

	response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
}
