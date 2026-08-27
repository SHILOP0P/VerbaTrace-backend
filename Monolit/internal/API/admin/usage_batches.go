package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type resetBatchService interface {
	CreateAllowanceResetBatch(context.Context, uuid.UUID, string, []uuid.UUID, string, string) (models.AllowanceResetBatch, error)
	ApproveAllowanceResetBatch(context.Context, uuid.UUID, uuid.UUID) (models.AllowanceResetBatch, error)
	ExecuteAllowanceResetBatch(context.Context, uuid.UUID, uuid.UUID) (models.AllowanceResetBatch, error)
	GetAllowanceResetBatch(context.Context, uuid.UUID, uuid.UUID) (models.AllowanceResetBatch, error)
	PreviewAllowanceResetBatch(context.Context, uuid.UUID, string, []uuid.UUID) (int, error)
}

func (h *Handler) PreviewUsageResetBatch(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	var req struct {
		OwnerType  string      `json:"owner_type"`
		OwnerUUIDs []uuid.UUID `json:"owner_uuids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		response.WriteError(w, 400, response.CodeInvalidAdminInput, "invalid preview")
		return
	}
	matched, err := h.service.(resetBatchService).PreviewAllowanceResetBatch(r.Context(), actor, req.OwnerType, req.OwnerUUIDs)
	if err != nil {
		writeAdminError(w, err, response.CodeInvalidAdminInput, "preview failed")
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"requested": len(req.OwnerUUIDs), "matched_active_subscriptions": matched, "requires_second_approval": matched > 100, "confirmation_text": fmt.Sprintf("RESET %d ACCOUNTS", matched)})
}

func (h *Handler) CreateUsageResetBatch(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	var req struct {
		OwnerType    string      `json:"owner_type"`
		OwnerUUIDs   []uuid.UUID `json:"owner_uuids"`
		Reason       string      `json:"reason"`
		Confirmation string      `json:"confirmation"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || len(req.OwnerUUIDs) == 0 || len(req.OwnerUUIDs) > 5000 || len(strings.TrimSpace(req.Reason)) < 3 {
		response.WriteError(w, 400, response.CodeInvalidAdminInput, "invalid reset batch")
		return
	}
	matched, err := h.service.(resetBatchService).PreviewAllowanceResetBatch(r.Context(), actor, req.OwnerType, req.OwnerUUIDs)
	if err != nil {
		writeAdminError(w, err, response.CodeInvalidAdminInput, "reset preview failed")
		return
	}
	if matched > 100 && strings.TrimSpace(req.Confirmation) != fmt.Sprintf("RESET %d ACCOUNTS", matched) {
		response.WriteError(w, 400, response.CodeInvalidAdminInput, "typed confirmation mismatch")
		return
	}
	item, err := h.service.(resetBatchService).CreateAllowanceResetBatch(r.Context(), actor, req.OwnerType, req.OwnerUUIDs, strings.TrimSpace(req.Reason), strings.TrimSpace(r.Header.Get("Idempotency-Key")))
	if err != nil {
		writeAdminError(w, err, response.CodeInvalidAdminInput, "failed to create reset batch")
		return
	}
	_ = response.WriteJSON(w, 201, item)
}
func (h *Handler) ApproveUsageResetBatch(w http.ResponseWriter, r *http.Request) {
	h.batchCommand(w, r, "approve")
}
func (h *Handler) ExecuteUsageResetBatch(w http.ResponseWriter, r *http.Request) {
	h.batchCommand(w, r, "execute")
}
func (h *Handler) batchCommand(w http.ResponseWriter, r *http.Request, command string) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "batch_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidAdminInput, "batch not found")
		return
	}
	var item models.AllowanceResetBatch
	if command == "approve" {
		item, err = h.service.(resetBatchService).ApproveAllowanceResetBatch(r.Context(), id, actor)
	} else {
		item, err = h.service.(resetBatchService).ExecuteAllowanceResetBatch(r.Context(), id, actor)
	}
	if err != nil {
		writeAdminError(w, err, response.CodeInvalidAdminInput, "reset batch command failed")
		return
	}
	_ = response.WriteJSON(w, 200, item)
}
func (h *Handler) GetUsageResetBatch(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "batch_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidAdminInput, "batch not found")
		return
	}
	item, err := h.service.(resetBatchService).GetAllowanceResetBatch(r.Context(), id, actor)
	if err != nil {
		writeAdminError(w, err, response.CodeInvalidAdminInput, "failed to get reset batch")
		return
	}
	_ = response.WriteJSON(w, 200, item)
}
