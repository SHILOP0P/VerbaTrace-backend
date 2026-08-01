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

	response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
}
