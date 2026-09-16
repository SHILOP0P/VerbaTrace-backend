package company

import (
	"errors"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// FreezeCompany parks a company the plan no longer covers.
func (h *Handler) FreezeCompany(w http.ResponseWriter, r *http.Request) {
	h.changeLifecycle(w, r, func(companyID, userID uuid.UUID) error {
		return h.service.FreezeCompany(r.Context(), companyID, userID)
	})
}

// ActivateCompany is how the owner picks which companies keep working.
func (h *Handler) ActivateCompany(w http.ResponseWriter, r *http.Request) {
	h.changeLifecycle(w, r, func(companyID, userID uuid.UUID) error {
		return h.service.ActivateCompany(r.Context(), companyID, userID)
	})
}

// GetCompanyLifecycle tells how long a frozen or deleted company has left.
func (h *Handler) GetCompanyLifecycle(w http.ResponseWriter, r *http.Request) {
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

	lifecycle, err := h.service.GetCompanyLifecycle(r.Context(), companyID, userID)
	if err != nil {
		writeLifecycleError(w, err)
		return
	}

	payload := dto.CompanyLifecycleResponse{
		CompanyUUID: lifecycle.CompanyUUID.String(),
		State:       string(lifecycle.State),
		RestoreUsed: lifecycle.RestoreUsed,
	}
	payload.FrozenAt = optionalTimestamp(lifecycle.FrozenAt)
	payload.SoftDeletedAt = optionalTimestamp(lifecycle.SoftDeletedAt)
	payload.PurgeAfter = optionalTimestamp(lifecycle.PurgeAfter)

	_ = response.WriteJSON(w, http.StatusOK, payload)
}

func (h *Handler) changeLifecycle(w http.ResponseWriter, r *http.Request, action func(companyID, userID uuid.UUID) error) {
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

	if err := action(companyID, userID); err != nil {
		writeLifecycleError(w, err)
		return
	}

	response.WriteNoContent(w)
}

func optionalTimestamp(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func writeLifecycleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrCompanyLimitExceeded):
		response.WriteError(w, http.StatusConflict, response.CodeCompanyLimitExceeded, "Тариф не покрывает ещё одну активную компанию")
	case errors.Is(err, models.ErrOwnerOnlyAction):
		response.WriteError(w, http.StatusForbidden, response.CodeOwnerOnlyAction, "Состояние компании меняет только владелец")
	case errors.Is(err, models.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	case errors.Is(err, models.ErrCompanyNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
	case errors.Is(err, models.ErrInvalidCompanyInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company input")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetCompany, "failed to change company state")
	}
}
