package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type auditTrailEntryResponse struct {
	OccurredAt string `json:"occurred_at"`
	// EntryUUID is only present where the record can be acted on, which today
	// means a billing alert waiting to be closed.
	EntryUUID     string `json:"entry_uuid,omitempty"`
	ActorUserUUID string `json:"actor_user_uuid,omitempty"`
	// ActorUsername is what the panel shows. The uuid above stays in the payload
	// for correlation with the raw tables, but it is not what a reader needs.
	ActorUsername string `json:"actor_username,omitempty"`
	Action        string `json:"action"`
	Details       any    `json:"details,omitempty"`
}

type resolveBillingAlertRequest struct {
	Reason string `json:"reason"`
}

type auditTrailListResponse struct {
	Trail  string                    `json:"trail"`
	Items  []auditTrailEntryResponse `json:"items"`
	Total  int                       `json:"total"`
	Limit  int                       `json:"limit"`
	Offset int                       `json:"offset"`
}

// ListAuditTrails names the records an administrator can open. Without it the
// panel would have to hard-code a list that only the backend really knows.
func (h *Handler) ListAuditTrails(w http.ResponseWriter, r *http.Request) {
	trails := models.AdminAuditTrails()
	names := make([]string, 0, len(trails))
	for _, trail := range trails {
		names = append(names, string(trail))
	}

	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"trails": names})
}

// GetAuditTrail reads one append-only record. These are the trails the system
// has always written and never shown.
func (h *Handler) GetAuditTrail(w http.ResponseWriter, r *http.Request) {
	trail := models.AdminAuditTrail(chi.URLParam(r, "trail"))
	if !trail.Valid() {
		response.WriteError(w, http.StatusNotFound, response.CodeInvalidAdminInput, "unknown audit trail")
		return
	}

	reader, ok := h.service.(interface {
		ListAuditTrail(ctx context.Context, input models.ListAdminAuditTrailInput) (models.ListAdminAuditTrailResult, error)
	})
	if !ok {
		response.WriteError(w, http.StatusNotImplemented, response.CodeInvalidAdminInput, "audit trails are not available")
		return
	}

	input := models.ListAdminAuditTrailInput{
		Trail:  trail,
		Limit:  intQuery(r, "limit", 50),
		Offset: intQuery(r, "offset", 0),
		From:   timeQuery(r, "from"),
		To:     timeQuery(r, "to"),
	}

	result, err := reader.ListAuditTrail(r.Context(), input)
	if err != nil {
		if errors.Is(err, models.ErrInvalidAdminInput) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid audit trail request")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeInvalidAdminInput, "failed to read the audit trail")
		return
	}

	items := make([]auditTrailEntryResponse, 0, len(result.Items))
	for _, entry := range result.Items {
		item := auditTrailEntryResponse{
			OccurredAt: entry.OccurredAt.UTC().Format(time.RFC3339),
			Action:     entry.Action,
		}
		if entry.EntryUUID.Valid {
			item.EntryUUID = entry.EntryUUID.UUID.String()
		}
		if entry.ActorUserUUID.Valid {
			item.ActorUserUUID = entry.ActorUserUUID.UUID.String()
		}
		if entry.ActorUsername.Valid {
			item.ActorUsername = entry.ActorUsername.String
		}
		if len(entry.Details) > 0 {
			item.Details = entry.Details
		}
		items = append(items, item)
	}

	_ = response.WriteJSON(w, http.StatusOK, auditTrailListResponse{
		Trail: string(result.Trail), Items: items, Total: result.Total, Limit: result.Limit, Offset: result.Offset,
	})
}

// ResolveBillingAlert closes an alert an administrator has handled. Alerts are
// the only trail with a state: the rest are history and cannot change.
func (h *Handler) ResolveBillingAlert(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	role, roleOK := middleware.UserRoleFromContext(r.Context())
	if !ok || !roleOK {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	alertUUID, err := uuid.Parse(chi.URLParam(r, "alert_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid alert uuid")
		return
	}

	var request resolveBillingAlertRequest
	if decodeErr := json.NewDecoder(r.Body).Decode(&request); decodeErr != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid request body")
		return
	}

	resolver, ok := h.service.(interface {
		ResolveBillingAlert(ctx context.Context, input models.ResolveBillingAlertInput, audit models.CreateAdminAuditLogInput) error
	})
	if !ok {
		response.WriteError(w, http.StatusNotImplemented, response.CodeInvalidAdminInput, "billing alerts are not available")
		return
	}

	metadata := adminMetadata(r, request.Reason)
	err = resolver.ResolveBillingAlert(r.Context(),
		models.ResolveBillingAlertInput{AlertUUID: alertUUID, RequestUser: actor, Reason: request.Reason},
		models.CreateAdminAuditLogInput{ActorRole: models.UserRole(role), RequestID: metadata.RequestID, IPAddress: metadata.IPAddress, UserAgent: metadata.UserAgent},
	)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrAdminReasonRequired):
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "reason is required")
		case errors.Is(err, models.ErrAdminRecordNotFound):
			response.WriteError(w, http.StatusNotFound, response.CodeInvalidAdminInput, "alert not found or already resolved")
		case errors.Is(err, models.ErrInvalidAdminInput):
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid alert request")
		default:
			response.WriteError(w, http.StatusInternalServerError, response.CodeInvalidAdminInput, "failed to resolve the alert")
		}
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"status": "resolved"})
}

func intQuery(r *http.Request, name string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return fallback
	}

	return value
}

func timeQuery(r *http.Request, name string) *time.Time {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}

	return &parsed
}
