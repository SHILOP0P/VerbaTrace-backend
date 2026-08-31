package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	bitrixService "verbatrace/monolit/internal/service/bitrix24"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Bitrix24Service interface {
	StartOAuth(context.Context, uuid.UUID, uuid.UUID, string, int64) (models.BitrixOAuthStart, error)
	CompleteOAuth(context.Context, string, string) (uuid.UUID, error)
	OAuthCallbackOrigin() string
	TestConnection(context.Context, uuid.UUID, uuid.UUID) (models.BitrixConnectionHealth, error)
	Health(context.Context, uuid.UUID, uuid.UUID) (models.BitrixConnectionHealth, error)
	ListExternalUsers(context.Context, uuid.UUID, uuid.UUID) ([]models.BitrixExternalUser, error)
	UpdateExternalUserMapping(context.Context, models.UpdateBitrixUserMappingInput) (models.BitrixExternalUser, error)
	PreviewExternalUserMappings(context.Context, uuid.UUID, uuid.UUID, []models.BitrixMappingChange) (models.BitrixMappingPreview, error)
	BulkUpdateExternalUserMappings(context.Context, models.BulkUpdateBitrixMappingsInput) (models.BitrixMappingBulkResult, error)
	CreateActionSync(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) (models.ActionExternalSync, bool, error)
	PreviewActionSync(context.Context, uuid.UUID, uuid.UUID) (models.ActionExternalSyncPreview, error)
	ApproveActionSync(context.Context, uuid.UUID, uuid.UUID, int64) (models.ActionExternalSync, error)
	RejectActionSync(context.Context, uuid.UUID, uuid.UUID, int64, string) error
	GetActionSync(context.Context, uuid.UUID, uuid.UUID) (models.ActionExternalSync, error)
	GetActionSyncByID(context.Context, uuid.UUID, uuid.UUID) (models.ActionExternalSync, error)
	ResolveActionSync(context.Context, models.ResolveActionExternalSyncInput) (models.ActionExternalSync, error)
	PauseConnection(context.Context, uuid.UUID, uuid.UUID, int64) (models.BitrixConnectionHealth, error)
	ResumeConnection(context.Context, uuid.UUID, uuid.UUID, int64) (models.BitrixConnectionHealth, error)
	PreviewBackfill(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) (models.BitrixBackfillPreview, error)
	CreateBackfill(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time, string) (models.BitrixBackfill, bool, error)
	GetBackfill(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (models.BitrixBackfill, error)
	ListBackfills(context.Context, uuid.UUID, uuid.UUID) ([]models.BitrixBackfill, error)
	AcceptEvent(context.Context, url.Values) (bool, error)
}

func (h *Handler) StartBitrix24OAuth(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", false)
		return
	}
	if h.bitrix24 == nil {
		writeError(w, http.StatusServiceUnavailable, "bitrix_connector_unavailable", true)
		return
	}
	version, err := parseIfMatch(r)
	if err != nil || version < 1 {
		writeError(w, http.StatusPreconditionRequired, "if_match_required", false)
		return
	}
	var req struct {
		ConnectionID uuid.UUID `json:"connection_uuid"`
		PortalDomain string    `json:"portal_domain"`
	}
	if err = decodeStrict(w, r, &req, 8<<10); err != nil {
		writeError(w, 400, "invalid_bitrix_input", false)
		return
	}
	item, err := h.bitrix24.StartOAuth(r.Context(), req.ConnectionID, actor, req.PortalDomain, version)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) CompleteBitrix24OAuth(w http.ResponseWriter, r *http.Request) {
	if h.bitrix24 == nil {
		writeError(w, http.StatusServiceUnavailable, "bitrix_connector_unavailable", true)
		return
	}
	if r.URL.Query().Get("error") != "" {
		writeError(w, http.StatusBadRequest, "bitrix_oauth_cancelled", false)
		return
	}
	id, err := h.bitrix24.CompleteOAuth(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"))
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	origin := h.bitrix24.OAuthCallbackOrigin()
	if origin == "" {
		_ = response.WriteJSON(w, http.StatusOK, map[string]any{"connection_uuid": id, "status": "testing"})
		return
	}
	originJSON, _ := json.Marshal(origin)
	idJSON, _ := json.Marshal(id.String())
	returnURL := html.EscapeString(origin + "/app/settings/integrations")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Bitrix24 подключён</title><style>body{margin:0;display:grid;place-items:center;min-height:100vh;font:16px system-ui;color:#2d211b;background:#fffaf6}.card{max-width:440px;margin:24px;padding:28px;border:1px solid #dfcfc5;border-radius:20px;text-align:center}a{color:#d84c24;font-weight:700}</style><main class="card"><h1>Авторизация завершена</h1><p>Подключение сохранено. Это окно можно закрыть.</p><a href="%s">Вернуться в VerbaTrace</a></main><script>const origin=%s;window.opener?.postMessage({type:"verbatrace:bitrix-oauth-complete",connection_uuid:%s,status:"testing"},origin);window.setTimeout(()=>window.close(),500);</script></html>`, returnURL, originJSON, idJSON)
}

func (h *Handler) AcceptBitrix24Event(w http.ResponseWriter, r *http.Request) {
	if h.bitrix24 == nil {
		writeError(w, http.StatusServiceUnavailable, "bitrix_connector_unavailable", true)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_bitrix_event", false)
		return
	}
	accepted, err := h.bitrix24.AcceptEvent(r.Context(), r.PostForm)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"accepted": accepted})
}

func (h *Handler) TestBitrix24Connection(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	item, err := h.bitrix24.TestConnection(r.Context(), id, actor)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) GetBitrix24Health(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	item, err := h.bitrix24.Health(r.Context(), id, actor)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) ListBitrix24ExternalUsers(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	items, err := h.bitrix24.ListExternalUsers(r.Context(), id, actor)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"users": items})
}

func (h *Handler) UpdateBitrix24ExternalUserMapping(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	version, err := parseIfMatch(r)
	if err != nil || version < 0 {
		writeError(w, http.StatusPreconditionRequired, "if_match_required", false)
		return
	}
	var req struct {
		InternalUserID *uuid.UUID `json:"internal_user_uuid"`
		DepartmentID   *uuid.UUID `json:"department_uuid"`
		Status         string     `json:"status"`
	}
	if err = decodeStrict(w, r, &req, 8<<10); err != nil {
		writeError(w, 400, "invalid_bitrix_mapping", false)
		return
	}
	in := models.UpdateBitrixUserMappingInput{ConnectionID: id, ExternalUserID: chi.URLParam(r, "external_user_id"), Status: req.Status, ActorID: actor, ExpectedLockVersion: version}
	if req.InternalUserID != nil {
		in.InternalUserID = uuid.NullUUID{UUID: *req.InternalUserID, Valid: true}
	}
	if req.DepartmentID != nil {
		in.DepartmentID = uuid.NullUUID{UUID: *req.DepartmentID, Valid: true}
	}
	item, err := h.bitrix24.UpdateExternalUserMapping(r.Context(), in)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) PreviewBitrix24ExternalUserMappings(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	changes, ok := decodeBitrixMappingChanges(w, r)
	if !ok {
		return
	}
	item, err := h.bitrix24.PreviewExternalUserMappings(r.Context(), id, actor, changes)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) BulkUpdateBitrix24ExternalUserMappings(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	changes, previewHash, ok := decodeBitrixMappingBulkCommand(w, r)
	if !ok {
		return
	}
	item, err := h.bitrix24.BulkUpdateExternalUserMappings(r.Context(), models.BulkUpdateBitrixMappingsInput{ConnectionID: id, ActorID: actor, PreviewHash: previewHash, RequestKey: r.Header.Get("Idempotency-Key"), Changes: changes})
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	status := http.StatusOK
	if item.Created {
		status = http.StatusCreated
	}
	_ = response.WriteJSON(w, status, item)
}

type bitrixMappingChangeRequest struct {
	ExternalUserID      string     `json:"external_user_id"`
	InternalUserID      *uuid.UUID `json:"internal_user_uuid"`
	DepartmentID        *uuid.UUID `json:"department_uuid"`
	Status              string     `json:"status"`
	ExpectedLockVersion int64      `json:"expected_lock_version"`
}

func decodeBitrixMappingChanges(w http.ResponseWriter, r *http.Request) ([]models.BitrixMappingChange, bool) {
	var req struct {
		Changes []bitrixMappingChangeRequest `json:"changes"`
	}
	if err := decodeStrict(w, r, &req, 256<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_bitrix_mapping", false)
		return nil, false
	}
	return mappingChangeModels(req.Changes), true
}

func decodeBitrixMappingBulkCommand(w http.ResponseWriter, r *http.Request) ([]models.BitrixMappingChange, string, bool) {
	var req struct {
		PreviewHash string                       `json:"preview_hash"`
		Changes     []bitrixMappingChangeRequest `json:"changes"`
	}
	if err := decodeStrict(w, r, &req, 256<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_bitrix_mapping", false)
		return nil, "", false
	}
	return mappingChangeModels(req.Changes), req.PreviewHash, true
}

func mappingChangeModels(items []bitrixMappingChangeRequest) []models.BitrixMappingChange {
	result := make([]models.BitrixMappingChange, 0, len(items))
	for _, item := range items {
		change := models.BitrixMappingChange{ExternalUserID: item.ExternalUserID, Status: item.Status, ExpectedLockVersion: item.ExpectedLockVersion}
		if item.InternalUserID != nil {
			change.InternalUserID = uuid.NullUUID{UUID: *item.InternalUserID, Valid: true}
		}
		if item.DepartmentID != nil {
			change.DepartmentID = uuid.NullUUID{UUID: *item.DepartmentID, Valid: true}
		}
		result = append(result, change)
	}
	return result
}

func (h *Handler) PauseBitrix24Connection(w http.ResponseWriter, r *http.Request) {
	h.changeBitrix24Lifecycle(w, r, false)
}

func (h *Handler) ResumeBitrix24Connection(w http.ResponseWriter, r *http.Request) {
	h.changeBitrix24Lifecycle(w, r, true)
}

func (h *Handler) changeBitrix24Lifecycle(w http.ResponseWriter, r *http.Request, resume bool) {
	actor, id, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	version, err := parseIfMatch(r)
	if err != nil || version < 1 {
		writeError(w, http.StatusPreconditionRequired, "if_match_required", false)
		return
	}
	var item models.BitrixConnectionHealth
	if resume {
		item, err = h.bitrix24.ResumeConnection(r.Context(), id, actor, version)
	} else {
		item, err = h.bitrix24.PauseConnection(r.Context(), id, actor, version)
	}
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) PreviewBitrix24Backfill(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	from, to, ok := decodeBackfillRange(w, r)
	if !ok {
		return
	}
	item, err := h.bitrix24.PreviewBackfill(r.Context(), id, actor, from, to)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) CreateBitrix24Backfill(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	from, to, ok := decodeBackfillRange(w, r)
	if !ok {
		return
	}
	item, created, err := h.bitrix24.CreateBackfill(r.Context(), id, actor, from, to, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	_ = response.WriteJSON(w, status, item)
}

func (h *Handler) GetBitrix24Backfill(w http.ResponseWriter, r *http.Request) {
	actor, connectionID, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	backfillID, err := uuid.Parse(chi.URLParam(r, "backfill_uuid"))
	if err != nil {
		writeError(w, http.StatusNotFound, "bitrix_backfill_not_found", false)
		return
	}
	item, err := h.bitrix24.GetBackfill(r.Context(), connectionID, backfillID, actor)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) ListBitrix24Backfills(w http.ResponseWriter, r *http.Request) {
	actor, connectionID, ok := bitrixActorAndConnection(w, r)
	if !ok {
		return
	}
	items, err := h.bitrix24.ListBackfills(r.Context(), connectionID, actor)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"backfills": items})
}

func decodeBackfillRange(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	var req struct {
		RangeFrom string `json:"range_from"`
		RangeTo   string `json:"range_to"`
	}
	if err := decodeStrict(w, r, &req, 8<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_bitrix_backfill", false)
		return time.Time{}, time.Time{}, false
	}
	from, fromErr := time.Parse(time.RFC3339, req.RangeFrom)
	to, toErr := time.Parse(time.RFC3339, req.RangeTo)
	if fromErr != nil || toErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_bitrix_backfill", false)
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

func (h *Handler) CreateActionExternalSync(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	actionID, err := uuid.Parse(chi.URLParam(r, "action_uuid"))
	if err != nil {
		writeError(w, 404, "action_not_found", false)
		return
	}
	var req struct {
		ConnectionID uuid.UUID `json:"connection_uuid"`
	}
	if err = decodeStrict(w, r, &req, 8<<10); err != nil {
		writeError(w, 400, "invalid_bitrix_input", false)
		return
	}
	item, created, err := h.bitrix24.CreateActionSync(r.Context(), actionID, req.ConnectionID, actor, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	_ = response.WriteJSON(w, status, item)
}

func (h *Handler) PreviewActionExternalSync(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", false)
		return
	}
	actionID, err := uuid.Parse(chi.URLParam(r, "action_uuid"))
	if err != nil {
		writeError(w, http.StatusNotFound, "action_not_found", false)
		return
	}
	item, err := h.bitrix24.PreviewActionSync(r.Context(), actionID, actor)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) ApproveActionExternalSync(w http.ResponseWriter, r *http.Request) {
	actor, id, version, ok := externalSyncMutationParams(w, r)
	if !ok {
		return
	}
	item, err := h.bitrix24.ApproveActionSync(r.Context(), id, actor, version)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, item)
}

func (h *Handler) RejectActionExternalSync(w http.ResponseWriter, r *http.Request) {
	actor, id, version, ok := externalSyncMutationParams(w, r)
	if !ok {
		return
	}
	var req struct {
		Comment string `json:"comment"`
	}
	if err := decodeStrict(w, r, &req, 8<<10); err != nil {
		writeError(w, 400, "invalid_bitrix_input", false)
		return
	}
	if err := h.bitrix24.RejectActionSync(r.Context(), id, actor, version, req.Comment); err != nil {
		writeBitrixError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ResolveActionExternalSync(w http.ResponseWriter, r *http.Request) {
	actor, id, version, ok := externalSyncMutationParams(w, r)
	if !ok {
		return
	}
	var req struct {
		Resolution     string `json:"resolution"`
		ExternalTaskID string `json:"external_task_id"`
		Reason         string `json:"reason"`
	}
	if err := decodeStrict(w, r, &req, 16<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_bitrix_input", false)
		return
	}
	item, err := h.bitrix24.ResolveActionSync(r.Context(), models.ResolveActionExternalSyncInput{SyncID: id, ActorID: actor, ExpectedLockVersion: version, Resolution: req.Resolution, ExternalTaskID: req.ExternalTaskID, Reason: req.Reason})
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) GetActionExternalSync(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	actionID, err := uuid.Parse(chi.URLParam(r, "action_uuid"))
	if err != nil {
		writeError(w, 404, "action_not_found", false)
		return
	}
	item, err := h.bitrix24.GetActionSync(r.Context(), actionID, actor)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, item)
}

func (h *Handler) GetActionExternalSyncRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", false)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "sync_uuid"))
	if err != nil {
		writeError(w, http.StatusNotFound, "bitrix_sync_not_found", false)
		return
	}
	item, err := h.bitrix24.GetActionSyncByID(r.Context(), id, actor)
	if err != nil {
		writeBitrixError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func externalSyncMutationParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, int64, bool) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return uuid.Nil, uuid.Nil, 0, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "sync_uuid"))
	if err != nil {
		writeError(w, 404, "bitrix_sync_not_found", false)
		return uuid.Nil, uuid.Nil, 0, false
	}
	version, err := parseIfMatch(r)
	if err != nil || version < 1 {
		writeError(w, http.StatusPreconditionRequired, "if_match_required", false)
		return uuid.Nil, uuid.Nil, 0, false
	}
	return actor, id, version, true
}

func bitrixActorAndConnection(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", false)
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		writeError(w, http.StatusNotFound, "bitrix_connection_not_found", false)
		return uuid.Nil, uuid.Nil, false
	}
	return actor, id, true
}

func parseIfMatch(r *http.Request) (int64, error) {
	return strconv.ParseInt(strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), "\""), 10, 64)
}

func writeBitrixError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, bitrixService.ErrInvalid):
		writeError(w, 400, "invalid_bitrix_input", false)
	case errors.Is(err, bitrixService.ErrOAuthState):
		writeError(w, 400, "bitrix_oauth_state_invalid", false)
	case errors.Is(err, bitrixService.ErrForbidden):
		writeError(w, 403, "bitrix_forbidden", false)
	case errors.Is(err, bitrixService.ErrNotFound):
		writeError(w, 404, "bitrix_connection_not_found", false)
	case errors.Is(err, bitrixService.ErrConflict):
		writeError(w, 409, "bitrix_connection_conflict", false)
	case errors.Is(err, bitrixService.ErrUnavailable):
		writeError(w, 503, "bitrix_connector_unavailable", true)
	default:
		writeError(w, 502, "bitrix_provider_error", true)
	}
}
