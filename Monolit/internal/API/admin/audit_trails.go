package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
)

type auditTrailEntryResponse struct {
	OccurredAt    string `json:"occurred_at"`
	ActorUserUUID string `json:"actor_user_uuid,omitempty"`
	Action        string `json:"action"`
	Details       any    `json:"details,omitempty"`
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
		if entry.ActorUserUUID.Valid {
			item.ActorUserUUID = entry.ActorUserUUID.UUID.String()
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
