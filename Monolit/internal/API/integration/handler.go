package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Service interface {
	CreateConnection(context.Context, models.CreateIntegrationConnectionInput) (models.IntegrationConnection, error)
	ListConnections(context.Context, uuid.UUID, uuid.UUID) ([]models.IntegrationConnection, error)
	GetConnection(context.Context, uuid.UUID, uuid.UUID) (models.IntegrationConnection, error)
	ChangeConnectionStatus(context.Context, uuid.UUID, uuid.UUID, string, int64) (models.IntegrationConnection, error)
	UpdateConnection(context.Context, models.UpdateIntegrationConnectionInput) (models.IntegrationConnection, error)
	Authenticate(context.Context, string, string, string) (models.IntegrationPrincipal, error)
	AcceptURLIngest(context.Context, models.IntegrationPrincipal, models.IngestCallInput, string) (models.IngestItem, bool, error)
	AcceptUploadIngest(context.Context, models.IntegrationPrincipal, models.IngestCallInput, string, io.Reader) (models.IngestItem, bool, error)
	GetIngest(context.Context, models.IntegrationPrincipal, uuid.UUID) (models.IngestItem, error)
	ListIngest(context.Context, uuid.UUID, uuid.UUID, int, int) ([]models.IngestItem, int, error)
	RetryIngest(context.Context, uuid.UUID, uuid.UUID) (models.IngestItem, error)
	CancelIngest(context.Context, uuid.UUID, uuid.UUID) (models.IngestItem, error)
	ListAudit(context.Context, uuid.UUID, uuid.UUID, int, int) ([]models.IntegrationAuditEvent, int, error)
	CreateWebhook(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, []string) (models.WebhookEndpoint, string, error)
	ListWebhooks(context.Context, uuid.UUID, uuid.UUID) ([]models.WebhookEndpoint, error)
	RevokeWebhook(context.Context, uuid.UUID, uuid.UUID) error
	ListDeliveries(context.Context, uuid.UUID, uuid.UUID) ([]models.WebhookDelivery, error)
	QueueWebhookTest(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error)
	ListDestinations(context.Context, models.IntegrationPrincipal) ([]models.IntegrationDestination, error)
	ListFolders(context.Context, models.IntegrationPrincipal, string, uuid.UUID, uuid.UUID) ([]models.IntegrationFolder, error)
	GetCall(context.Context, models.IntegrationPrincipal, uuid.UUID) (models.IntegrationCallView, error)
	GetTranscription(context.Context, models.IntegrationPrincipal, uuid.UUID) (models.IntegrationTranscriptionView, error)
	GetAnalysis(context.Context, models.IntegrationPrincipal, uuid.UUID) (models.IntegrationAnalysisView, error)
}
type Handler struct{ service Service }

func NewHandler(s Service) *Handler { return &Handler{service: s} }

func (h *Handler) CreateConnection(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", false)
		return
	}
	app, err := uuid.Parse(chi.URLParam(r, "application_uuid"))
	if err != nil {
		writeError(w, 400, "invalid_application_uuid", false)
		return
	}
	var req struct {
		Name                string          `json:"name"`
		Provider            string          `json:"provider"`
		CompanyID           *uuid.UUID      `json:"company_uuid"`
		DepartmentID        *uuid.UUID      `json:"department_uuid"`
		FolderID            *uuid.UUID      `json:"folder_uuid"`
		DisablePolicy       string          `json:"disable_policy"`
		AllowFolderOverride bool            `json:"allow_folder_override"`
		Settings            json.RawMessage `json:"settings"`
	}
	if err = decodeStrict(w, r, &req, 64<<10); err != nil {
		writeError(w, 400, "invalid_request", false)
		return
	}
	in := models.CreateIntegrationConnectionInput{ApplicationID: app, ActorID: actor, Name: req.Name, Provider: req.Provider, DisablePolicy: req.DisablePolicy, AllowFolderOverride: req.AllowFolderOverride, Settings: req.Settings}
	if req.CompanyID != nil {
		in.CompanyID = uuid.NullUUID{UUID: *req.CompanyID, Valid: true}
	}
	if req.DepartmentID != nil {
		in.DepartmentID = uuid.NullUUID{UUID: *req.DepartmentID, Valid: true}
	}
	if req.FolderID != nil {
		in.FolderID = uuid.NullUUID{UUID: *req.FolderID, Valid: true}
	}
	item, err := h.service.CreateConnection(r.Context(), in)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 201, item)
}
func (h *Handler) ListConnections(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	app, err := uuid.Parse(chi.URLParam(r, "application_uuid"))
	if err != nil {
		writeError(w, 400, "invalid_application_uuid", false)
		return
	}
	items, err := h.service.ListConnections(r.Context(), app, actor)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"connections": items})
}
func (h *Handler) GetConnection(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		writeError(w, 404, "integration_not_found", false)
		return
	}
	item, err := h.service.GetConnection(r.Context(), id, actor)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, item)
}
func (h *Handler) UpdateConnection(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		writeError(w, 404, "integration_not_found", false)
		return
	}
	version, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), "\""), 10, 64)
	if err != nil || version < 1 {
		writeError(w, http.StatusPreconditionRequired, "if_match_required", false)
		return
	}
	var req struct {
		Name          string          `json:"name"`
		DisablePolicy string          `json:"disable_policy"`
		Settings      json.RawMessage `json:"settings"`
	}
	if err = decodeStrict(w, r, &req, 64<<10); err != nil {
		writeError(w, 400, "invalid_request", false)
		return
	}
	item, err := h.service.UpdateConnection(r.Context(), models.UpdateIntegrationConnectionInput{ConnectionID: id, ActorID: actor, Name: req.Name, DisablePolicy: req.DisablePolicy, Settings: req.Settings, ExpectedLockVersion: version})
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, item)
}
func (h *Handler) EnableConnection(w http.ResponseWriter, r *http.Request) {
	h.connectionStatus(w, r, "active")
}
func (h *Handler) DisableConnection(w http.ResponseWriter, r *http.Request) {
	h.connectionStatus(w, r, "disabled")
}
func (h *Handler) RevokeConnection(w http.ResponseWriter, r *http.Request) {
	h.connectionStatus(w, r, "revoked")
}
func (h *Handler) connectionStatus(w http.ResponseWriter, r *http.Request, status string) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		writeError(w, 404, "integration_not_found", false)
		return
	}
	version, _ := strconv.ParseInt(strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), "\""), 10, 64)
	item, err := h.service.ChangeConnectionStatus(r.Context(), id, actor, status, version)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, item)
}

func (h *Handler) IngestSandbox(w http.ResponseWriter, r *http.Request) { h.ingest(w, r, "sandbox") }
func (h *Handler) IngestProduction(w http.ResponseWriter, r *http.Request) {
	h.ingest(w, r, "production")
}
func (h *Handler) UploadSandbox(w http.ResponseWriter, r *http.Request) {
	h.upload(w, r, "sandbox")
}
func (h *Handler) UploadProduction(w http.ResponseWriter, r *http.Request) {
	h.upload(w, r, "production")
}
func (h *Handler) ingest(w http.ResponseWriter, r *http.Request, environment string) {
	key := bearer(r.Header.Get("Authorization"))
	p, err := h.service.Authenticate(r.Context(), key, environment, "calls:write")
	if err != nil {
		authError(w, err)
		return
	}
	var in models.IngestCallInput
	if err = decodeStrict(w, r, &in, 256<<10); err != nil {
		writeError(w, 400, "invalid_request", false)
		return
	}
	item, dedup, err := h.service.AcceptURLIngest(r.Context(), p, in, r.Header.Get("Idempotency-Key"))
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 202, ingestResponse(item, dedup, environment, apiVersion(r)))
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request, environment string) {
	p, err := h.service.Authenticate(r.Context(), bearer(r.Header.Get("Authorization")), environment, "calls:write")
	if err != nil {
		authError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, (500<<20)+(1<<20))
	if err = r.ParseMultipartForm(1 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_multipart", false)
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	if len(r.MultipartForm.Value) != 1 || len(r.MultipartForm.Value["metadata"]) != 1 || len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["media"]) != 1 {
		writeError(w, http.StatusBadRequest, "invalid_multipart", false)
		return
	}
	metadata := []byte(r.MultipartForm.Value["metadata"][0])
	if len(metadata) == 0 || len(metadata) > 256<<10 || rejectDuplicateKeys(metadata) != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", false)
		return
	}
	var in models.IngestCallInput
	dec := json.NewDecoder(bytes.NewReader(metadata))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&in); err != nil || dec.Decode(&struct{}{}) != io.EOF || strings.TrimSpace(in.RecordingURL) != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", false)
		return
	}
	fileHeader := r.MultipartForm.File["media"][0]
	if fileHeader.Size <= 0 || fileHeader.Size > 500<<20 {
		writeError(w, http.StatusBadRequest, "invalid_media_size", false)
		return
	}
	media, err := fileHeader.Open()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_media", false)
		return
	}
	defer func() { _ = media.Close() }()
	if strings.TrimSpace(in.OriginalFilename) == "" {
		in.OriginalFilename = fileHeader.Filename
	}
	item, deduplicated, err := h.service.AcceptUploadIngest(r.Context(), p, in, r.Header.Get("Idempotency-Key"), media)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusAccepted, ingestResponse(item, deduplicated, environment, apiVersion(r)))
}
func (h *Handler) GetSandboxIngest(w http.ResponseWriter, r *http.Request) {
	h.getIngest(w, r, "sandbox")
}
func (h *Handler) GetProductionIngest(w http.ResponseWriter, r *http.Request) {
	h.getIngest(w, r, "production")
}

func (h *Handler) ListSandboxDestinations(w http.ResponseWriter, r *http.Request) {
	h.listDestinations(w, r, "sandbox")
}
func (h *Handler) ListProductionDestinations(w http.ResponseWriter, r *http.Request) {
	h.listDestinations(w, r, "production")
}
func (h *Handler) listDestinations(w http.ResponseWriter, r *http.Request, environment string) {
	p, err := h.service.Authenticate(r.Context(), bearer(r.Header.Get("Authorization")), environment, "destinations:read")
	if err != nil {
		authError(w, err)
		return
	}
	items, err := h.service.ListDestinations(r.Context(), p)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"destinations": items})
}
func (h *Handler) ListSandboxFolders(w http.ResponseWriter, r *http.Request) {
	h.listFolders(w, r, "sandbox")
}
func (h *Handler) ListProductionFolders(w http.ResponseWriter, r *http.Request) {
	h.listFolders(w, r, "production")
}
func (h *Handler) listFolders(w http.ResponseWriter, r *http.Request, environment string) {
	p, err := h.service.Authenticate(r.Context(), bearer(r.Header.Get("Authorization")), environment, "destinations:read")
	if err != nil {
		authError(w, err)
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	companyID, departmentID := uuid.Nil, uuid.Nil
	if raw := strings.TrimSpace(r.URL.Query().Get("company_uuid")); raw != "" {
		companyID, err = uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_destination", false)
			return
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("department_uuid")); raw != "" {
		departmentID, err = uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_destination", false)
			return
		}
	}
	items, err := h.service.ListFolders(r.Context(), p, scope, companyID, departmentID)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"folders": items})
}

func (h *Handler) GetSandboxCall(w http.ResponseWriter, r *http.Request) {
	h.getCall(w, r, "sandbox")
}
func (h *Handler) GetProductionCall(w http.ResponseWriter, r *http.Request) {
	h.getCall(w, r, "production")
}
func (h *Handler) getCall(w http.ResponseWriter, r *http.Request, environment string) {
	p, id, ok := h.readCallPrincipal(w, r, environment)
	if !ok {
		return
	}
	item, err := h.service.GetCall(r.Context(), p, id)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) GetSandboxTranscription(w http.ResponseWriter, r *http.Request) {
	h.getTranscription(w, r, "sandbox")
}
func (h *Handler) GetProductionTranscription(w http.ResponseWriter, r *http.Request) {
	h.getTranscription(w, r, "production")
}
func (h *Handler) getTranscription(w http.ResponseWriter, r *http.Request, environment string) {
	p, id, ok := h.readCallPrincipal(w, r, environment)
	if !ok {
		return
	}
	item, err := h.service.GetTranscription(r.Context(), p, id)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) GetSandboxAnalysis(w http.ResponseWriter, r *http.Request) {
	h.getAnalysis(w, r, "sandbox")
}
func (h *Handler) GetProductionAnalysis(w http.ResponseWriter, r *http.Request) {
	h.getAnalysis(w, r, "production")
}
func (h *Handler) getAnalysis(w http.ResponseWriter, r *http.Request, environment string) {
	p, id, ok := h.readCallPrincipal(w, r, environment)
	if !ok {
		return
	}
	item, err := h.service.GetAnalysis(r.Context(), p, id)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) readCallPrincipal(w http.ResponseWriter, r *http.Request, environment string) (models.IntegrationPrincipal, uuid.UUID, bool) {
	p, err := h.service.Authenticate(r.Context(), bearer(r.Header.Get("Authorization")), environment, "calls:read")
	if err != nil {
		authError(w, err)
		return models.IntegrationPrincipal{}, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "call_uuid"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", false)
		return models.IntegrationPrincipal{}, uuid.Nil, false
	}
	return p, id, true
}

func (h *Handler) getIngest(w http.ResponseWriter, r *http.Request, environment string) {
	p, err := h.service.Authenticate(r.Context(), bearer(r.Header.Get("Authorization")), environment, "calls:read")
	if err != nil {
		authError(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "ingest_item_uuid"))
	if err != nil {
		writeError(w, 404, "ingest_not_found", false)
		return
	}
	item, err := h.service.GetIngest(r.Context(), p, id)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, ingestResponse(item, false, environment, apiVersion(r)))
}

func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	connection, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		writeError(w, 404, "not_found", false)
		return
	}
	var req struct {
		ApplicationID uuid.UUID `json:"application_uuid"`
		Name          string    `json:"name"`
		URL           string    `json:"url"`
		EventTypes    []string  `json:"event_types"`
	}
	if err = decodeStrict(w, r, &req, 32<<10); err != nil {
		writeError(w, 400, "invalid_request", false)
		return
	}
	item, secret, err := h.service.CreateWebhook(r.Context(), req.ApplicationID, connection, actor, req.Name, req.URL, req.EventTypes)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 201, map[string]any{"webhook": item, "signing_secret": secret, "secret_visible_once": true})
}
func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	connection, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		writeError(w, 404, "not_found", false)
		return
	}
	items, err := h.service.ListWebhooks(r.Context(), connection, actor)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"webhooks": items})
}
func (h *Handler) RevokeWebhook(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "webhook_uuid"))
	if err != nil {
		writeError(w, 404, "not_found", false)
		return
	}
	if err = h.service.RevokeWebhook(r.Context(), id, actor); err != nil {
		integrationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	connection, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		writeError(w, 404, "not_found", false)
		return
	}
	items, err := h.service.ListDeliveries(r.Context(), connection, actor)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"deliveries": items})
}
func (h *Handler) TestWebhook(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	connection, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		writeError(w, 404, "not_found", false)
		return
	}
	eventID, err := h.service.QueueWebhookTest(r.Context(), connection, actor)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 202, map[string]any{"event_uuid": eventID, "status": "queued"})
}

func (h *Handler) ListIngestItems(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	connection, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		writeError(w, 404, "not_found", false)
		return
	}
	limit, offset := 10, 0
	if value, parseErr := strconv.Atoi(r.URL.Query().Get("limit")); parseErr == nil && value > 0 && value <= 100 {
		limit = value
	}
	if value, parseErr := strconv.Atoi(r.URL.Query().Get("offset")); parseErr == nil && value >= 0 {
		offset = value
	}
	items, total, err := h.service.ListIngest(r.Context(), connection, actor, limit, offset)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"ingest_items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) RetryIngestItem(w http.ResponseWriter, r *http.Request) {
	h.ingestCommand(w, r, true)
}
func (h *Handler) CancelIngestItem(w http.ResponseWriter, r *http.Request) {
	h.ingestCommand(w, r, false)
}
func (h *Handler) ingestCommand(w http.ResponseWriter, r *http.Request, retry bool) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "ingest_item_uuid"))
	if err != nil {
		writeError(w, 404, "not_found", false)
		return
	}
	var item models.IngestItem
	if retry {
		item, err = h.service.RetryIngest(r.Context(), id, actor)
	} else {
		item, err = h.service.CancelIngest(r.Context(), id, actor)
	}
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, item)
}

func (h *Handler) ListAuditEvents(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	connection, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		writeError(w, 404, "not_found", false)
		return
	}
	limit, offset := queryPage(r, 10, 100)
	events, total, err := h.service.ListAudit(r.Context(), connection, actor, limit, offset)
	if err != nil {
		integrationError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"audit_events": events, "total": total, "limit": limit, "offset": offset})
}

func queryPage(r *http.Request, defaultLimit, maxLimit int) (int, int) {
	limit, offset := defaultLimit, 0
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= maxLimit {
		limit = value
	}
	if value, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && value >= 0 {
		offset = value
	}
	return limit, offset
}

func ingestResponse(i models.IngestItem, d bool, e string, version int) map[string]any {
	return map[string]any{"ingest_item_uuid": i.ID, "status": i.Status, "stage": i.Stage, "deduplicated": d, "status_url": fmt.Sprintf("/api/%s/v%d/ingest/items/%s", e, version, i.ID), "call_uuid": i.CallID, "destination_scope": i.DestinationScope, "destination_folder_uuid": i.DestinationFolderID}
}
func apiVersion(r *http.Request) int {
	if strings.Contains(r.URL.Path, "/v2/") {
		return 2
	}
	return 1
}
func bearer(v string) string {
	if !strings.HasPrefix(v, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(v, "Bearer "))
}
func decodeStrict(w http.ResponseWriter, r *http.Request, dst any, max int64) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		return err
	}
	if err = rejectDuplicateKeys(raw); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(dst); err != nil {
		return err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing json")
	}
	return nil
}
func rejectDuplicateKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{':
				seen := map[string]bool{}
				for dec.More() {
					k, err := dec.Token()
					if err != nil {
						return err
					}
					key := k.(string)
					if seen[key] {
						return errors.New("duplicate json key")
					}
					seen[key] = true
					if err = walk(); err != nil {
						return err
					}
				}
				_, err = dec.Token()
				return err
			case '[':
				for dec.More() {
					if err = walk(); err != nil {
						return err
					}
				}
				_, err = dec.Token()
				return err
			}
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing json")
	}
	return nil
}
func integrationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrInvalidBillingInput):
		writeError(w, 400, "invalid_request", false)
	case errors.Is(err, models.ErrRecordingURLForbidden):
		writeError(w, 400, "recording_url_forbidden", false)
	case errors.Is(err, models.ErrForbidden):
		writeError(w, 403, "forbidden", false)
	case errors.Is(err, models.ErrAPIKeyScopeDenied):
		writeError(w, 403, "api_key_scope_denied", false)
	case errors.Is(err, models.ErrIntegrationNotFound), errors.Is(err, models.ErrIngestNotFound):
		writeError(w, 404, "not_found", false)
	case errors.Is(err, models.ErrIntegrationConflict):
		writeError(w, 409, "idempotency_conflict", false)
	case errors.Is(err, models.ErrIntegrationDisabled):
		writeError(w, 409, "integration_disabled", false)
	case errors.Is(err, models.ErrApplicationBudgetExceeded):
		writeError(w, 429, "application_budget_exceeded", true)
	default:
		writeError(w, 500, "integration_internal_error", true)
	}
}
func authError(w http.ResponseWriter, err error) {
	if errors.Is(err, models.ErrAPIKeyEnvironmentMismatch) {
		writeError(w, 401, "key_environment_mismatch", false)
	} else if errors.Is(err, models.ErrAPIKeyScopeDenied) {
		writeError(w, 403, "api_key_scope_denied", false)
	} else {
		writeError(w, 401, "invalid_api_key", false)
	}
}
func writeError(w http.ResponseWriter, status int, code string, retry bool) {
	requestID := uuid.NewString()
	if existing := strings.TrimSpace(w.Header().Get("X-Request-ID")); existing != "" {
		requestID = existing
	} else {
		w.Header().Set("X-Request-ID", requestID)
	}
	_ = response.WriteJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": http.StatusText(status), "request_id": requestID, "retryable": retry}})
}
