package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// CompanyLifecycleService is the part of the company service the superadmin
// section needs. It is optional, so a deployment without it simply has no
// restore route rather than a broken one.
type CompanyLifecycleService interface {
	RestoreSoftDeletedCompany(ctx context.Context, companyID uuid.UUID) error
	GetCompanyLifecycle(ctx context.Context, companyID uuid.UUID, requestUser uuid.UUID) (models.CompanyLifecycle, error)
}

func (h *Handler) SetCompanyLifecycleService(service CompanyLifecycleService) {
	h.companyLifecycle = service
}

// RestoreCompany brings a soft-deleted company back to a freeze, once.
//
// It is the last chance to undo a deletion, and until now it existed only in the
// repository and the service: no handler, no route, no way to reach it. It is the
// superadmin's decision alone, and it gives nobody access to the content — the
// company returns frozen, exactly as it was before the deletion started.
func (h *Handler) RestoreCompany(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	if !isSuperAdmin(r) {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "restoring a deleted company is the superadmin's decision")
		return
	}
	if h.companyLifecycle == nil {
		response.WriteError(w, http.StatusServiceUnavailable, response.CodeInternalServerError, "company lifecycle is not available")
		return
	}
	companyID, ok := adminCompanyID(w, r)
	if !ok {
		return
	}

	var req dto.RestoreAdminCompanyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		response.WriteError(w, http.StatusBadRequest, response.CodeAdminReasonRequired, "reason is required")
		return
	}

	if err := h.companyLifecycle.RestoreSoftDeletedCompany(r.Context(), companyID); err != nil {
		switch {
		case errors.Is(err, models.ErrCompanyNotFound):
			response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "no soft-deleted company to restore")
		case errors.Is(err, models.ErrInvalidCompanyInput):
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid company")
		default:
			response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "failed to restore the company")
		}
		return
	}

	metadata := adminMetadata(r, req.Reason)
	if _, err := h.service.RecordAudit(r.Context(), models.CreateAdminAuditLogInput{
		ActorUserUUID: actor,
		Action:        "company.restored",
		TargetType:    "company",
		TargetUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		Reason:        &metadata.Reason,
		RequestID:     metadata.RequestID,
		IPAddress:     metadata.IPAddress,
		UserAgent:     metadata.UserAgent,
	}); err != nil {
		// The company is already back. A missing audit line is worth reporting,
		// not worth undoing the rescue.
		response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "company restored but the audit record failed")
		return
	}

	lifecycle, err := h.companyLifecycle.GetCompanyLifecycle(r.Context(), companyID, actor)
	if err != nil {
		_ = response.WriteJSON(w, http.StatusOK, map[string]any{"company_uuid": companyID.String()})
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, dto.AdminCompanyLifecycleResponse{
		CompanyUUID:  lifecycle.CompanyUUID.String(),
		State:        string(lifecycle.State),
		FreezeReason: string(lifecycle.FreezeReason),
		RestoreUsed:  lifecycle.RestoreUsed,
	})
}
