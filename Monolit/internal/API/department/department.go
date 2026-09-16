package department

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
	service service.DepartmentService
}

func NewDepartmentHandler(service service.DepartmentService) *Handler {
	return &Handler{service: service}
}

func userIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return middleware.UserIDFromContext(r.Context())
}

// writeDepartmentError maps domain errors of the department flows onto the API
// error contract.
func writeDepartmentError(w http.ResponseWriter, err error, fallbackCode string, fallbackMessage string) {
	switch {
	case errors.Is(err, models.ErrInvalidDepartmentInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidDepartmentInput, "invalid department input")
	case errors.Is(err, models.ErrInvalidCompanyInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company input")
	case errors.Is(err, models.ErrDepartmentMembershipConflict):
		response.WriteError(w, http.StatusConflict, "department_membership_conflict", "Сотрудник уже состоит в другом отделе. Используйте перевод.")
	case errors.Is(err, models.ErrDepartmentTransferPending):
		response.WriteError(w, http.StatusConflict, response.CodeDepartmentTransferPending, "transfer request is already pending")
	case errors.Is(err, models.ErrDepartmentTransferNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeDepartmentTransferNotFound, "transfer request not found")
	case errors.Is(err, models.ErrCompanyNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
	case errors.Is(err, models.ErrDepartmentNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeDepartmentNotFound, "department not found")
	case errors.Is(err, models.ErrOwnerOnlyAction):
		response.WriteError(w, http.StatusForbidden, response.CodeOwnerOnlyAction, "action is available to the company owner only")
	case errors.Is(err, models.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	case errors.Is(err, models.ErrSubscriptionRequired):
		response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
	default:
		response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
	}
}
