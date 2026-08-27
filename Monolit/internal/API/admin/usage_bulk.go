package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"

	"github.com/google/uuid"
)

// BulkResetUsage is deliberately explicit: callers submit the exact targets,
// every target gets its own serializable reset and audit result.
func (h *Handler) BulkResetUsage(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	var req struct {
		OwnerType  string      `json:"owner_type"`
		OwnerUUIDs []uuid.UUID `json:"owner_uuids"`
		Reason     string      `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil || len(req.OwnerUUIDs) == 0 || len(req.OwnerUUIDs) > 500 || len(strings.TrimSpace(req.Reason)) < 3 || (req.OwnerType != "user" && req.OwnerType != "company") {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAdminInput, "invalid bulk reset input")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	batch, err := h.service.(resetBatchService).CreateAllowanceResetBatch(r.Context(), actor, req.OwnerType, req.OwnerUUIDs, strings.TrimSpace(req.Reason), key)
	if err != nil {
		writeAdminError(w, err, response.CodeInvalidAdminInput, "failed to create reset batch")
		return
	}
	if batch.RequiresSecondApproval {
		_ = response.WriteJSON(w, http.StatusAccepted, batch)
		return
	}
	batch, err = h.service.(resetBatchService).ExecuteAllowanceResetBatch(r.Context(), batch.ID, actor)
	if err != nil {
		writeAdminError(w, err, response.CodeInvalidAdminInput, "failed to execute reset batch")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, batch)
}
