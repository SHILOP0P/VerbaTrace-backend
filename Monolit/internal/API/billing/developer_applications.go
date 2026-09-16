package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type developerService interface {
	CreateDeveloperApplication(context.Context, models.CreateDeveloperApplicationInput) (models.DeveloperApplication, error)
	ListDeveloperApplications(context.Context, string, uuid.UUID, uuid.UUID) ([]models.DeveloperApplication, error)
	CreateIntegrationAPIKey(context.Context, uuid.UUID, uuid.UUID, models.CreateIntegrationAPIKeyInput) (models.IntegrationAPIKey, string, error)
	AuthenticateIntegrationKey(context.Context, string, string, string) (models.IntegrationPrincipal, error)
	RevokeIntegrationAPIKey(context.Context, uuid.UUID, uuid.UUID) error
	RotateIntegrationAPIKey(context.Context, uuid.UUID, uuid.UUID, time.Duration) (models.IntegrationAPIKey, string, error)
	CreateIntegrationServiceAccount(context.Context, uuid.UUID, uuid.UUID, string, []string) (models.IntegrationServiceAccount, error)
	ListIntegrationServiceAccounts(context.Context, uuid.UUID, uuid.UUID) ([]models.IntegrationServiceAccount, error)
	CreateIntegrationAPIKeyForServiceAccount(context.Context, uuid.UUID, uuid.UUID, models.CreateIntegrationAPIKeyInput) (models.IntegrationAPIKey, string, error)
	GetDeveloperApplication(context.Context, uuid.UUID, uuid.UUID) (models.DeveloperApplication, error)
	ChangeDeveloperApplicationStatus(context.Context, uuid.UUID, uuid.UUID, string) (models.DeveloperApplication, error)
	AdjustSandboxWallet(context.Context, uuid.UUID, uuid.UUID, string, int64, string) (int64, error)
	GetSandboxWallet(context.Context, uuid.UUID, uuid.UUID) (models.SandboxWalletDashboard, error)
	ListIntegrationAPIKeys(context.Context, uuid.UUID, uuid.UUID) ([]models.IntegrationAPIKey, error)
	UpdateDeveloperApplication(context.Context, models.UpdateDeveloperApplicationInput) (models.DeveloperApplication, error)
	RevokeIntegrationServiceAccount(context.Context, uuid.UUID, uuid.UUID) error
}

func (h *Handler) RevokeIntegrationServiceAccount(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "service_account_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusNotFound, response.CodeInvalidBillingInput, "service account not found")
		return
	}
	if err = h.service.(developerService).RevokeIntegrationServiceAccount(r.Context(), serviceID, actor); err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "failed to revoke service account")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateDeveloperApplication(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	appID, err := uuid.Parse(chi.URLParam(r, "application_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusNotFound, response.CodeInvalidBillingInput, "application not found")
		return
	}
	version, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), "\""), 10, 64)
	if err != nil || version < 1 {
		response.WriteError(w, http.StatusPreconditionRequired, response.CodeInvalidBillingInput, "If-Match is required")
		return
	}
	var req struct {
		Name                   string   `json:"name"`
		Capabilities           []string `json:"capabilities"`
		DailyCreditLimit       *int64   `json:"daily_credit_limit"`
		MonthlyCreditLimit     *int64   `json:"monthly_credit_limit"`
		MaxCreditsPerOperation *int64   `json:"max_credits_per_operation"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" || !validCapabilities(req.Capabilities) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid request")
		return
	}
	app, err := h.service.(developerService).UpdateDeveloperApplication(r.Context(), models.UpdateDeveloperApplicationInput{ApplicationUUID: appID, ActorUUID: actor, Name: req.Name, Capabilities: req.Capabilities, DailyCreditLimit: req.DailyCreditLimit, MonthlyCreditLimit: req.MonthlyCreditLimit, MaxCreditsPerOperation: req.MaxCreditsPerOperation, ExpectedLockVersion: version})
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "application update failed")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, developerApplicationResponse(app))
}

func (h *Handler) ListServiceAccountAPIKeys(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "service_account_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidBillingInput, "service account not found")
		return
	}
	items, err := h.service.(developerService).ListIntegrationAPIKeys(r.Context(), serviceID, actor)
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "failed to list api keys")
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"keys": items})
}

func (h *Handler) AdjustSandboxWallet(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	appID, err := uuid.Parse(chi.URLParam(r, "application_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidBillingInput, "application not found")
		return
	}
	var req struct {
		Mode    string `json:"mode"`
		Credits int64  `json:"credits"`
	}
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		response.WriteError(w, 400, response.CodeInvalidBillingInput, "invalid request")
		return
	}
	balance, err := h.service.(developerService).AdjustSandboxWallet(r.Context(), appID, actor, req.Mode, req.Credits, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "sandbox wallet adjustment failed")
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"application_uuid": appID, "environment": "sandbox", "balance_credits": balance})
}

func (h *Handler) GetSandboxWallet(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	appID, err := uuid.Parse(chi.URLParam(r, "application_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusNotFound, response.CodeInvalidBillingInput, "application not found")
		return
	}
	wallet, err := h.service.(developerService).GetSandboxWallet(r.Context(), appID, actor)
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "sandbox wallet not found")
		return
	}
	entries := make([]map[string]any, 0, len(wallet.Entries))
	for _, entry := range wallet.Entries {
		entries = append(entries, map[string]any{"transaction_uuid": entry.TransactionUUID, "type": entry.Type, "credits": entry.Credits, "reason": entry.Reason, "created_at": entry.CreatedAt})
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{
		"application_uuid": wallet.ApplicationUUID,
		"application_name": wallet.ApplicationName,
		"environment":      "sandbox",
		"balance_credits":  wallet.BalanceCredits,
		"entries":          entries,
	})
}

func (h *Handler) GetDeveloperApplication(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	appID, err := uuid.Parse(chi.URLParam(r, "application_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidBillingInput, "application not found")
		return
	}
	app, err := h.service.(developerService).GetDeveloperApplication(r.Context(), appID, actor)
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "application not found")
		return
	}
	_ = response.WriteJSON(w, 200, developerApplicationResponse(app))
}
func (h *Handler) DisableDeveloperApplication(w http.ResponseWriter, r *http.Request) {
	h.changeDeveloperApplicationStatus(w, r, "disabled")
}
func (h *Handler) EnableDeveloperApplication(w http.ResponseWriter, r *http.Request) {
	h.changeDeveloperApplicationStatus(w, r, "active")
}
func (h *Handler) RevokeDeveloperApplication(w http.ResponseWriter, r *http.Request) {
	h.changeDeveloperApplicationStatus(w, r, "revoked")
}
func (h *Handler) changeDeveloperApplicationStatus(w http.ResponseWriter, r *http.Request, status string) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	appID, err := uuid.Parse(chi.URLParam(r, "application_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidBillingInput, "application not found")
		return
	}
	app, err := h.service.(developerService).ChangeDeveloperApplicationStatus(r.Context(), appID, actor, status)
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "application status change failed")
		return
	}
	_ = response.WriteJSON(w, 200, developerApplicationResponse(app))
}

func (h *Handler) CreateIntegrationServiceAccount(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	connection, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidBillingInput, "connection not found")
		return
	}
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" || !validScopes(req.Scopes) {
		response.WriteError(w, 400, response.CodeInvalidBillingInput, "invalid request")
		return
	}
	item, err := h.service.(developerService).CreateIntegrationServiceAccount(r.Context(), connection, actor, req.Name, req.Scopes)
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "failed to create service account")
		return
	}
	_ = response.WriteJSON(w, 201, item)
}
func (h *Handler) ListIntegrationServiceAccounts(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	connection, err := uuid.Parse(chi.URLParam(r, "connection_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidBillingInput, "connection not found")
		return
	}
	items, err := h.service.(developerService).ListIntegrationServiceAccounts(r.Context(), connection, actor)
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "failed to list service accounts")
		return
	}
	_ = response.WriteJSON(w, 200, map[string]any{"service_accounts": items})
}
func (h *Handler) CreateServiceAccountAPIKey(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	serviceID, err := uuid.Parse(chi.URLParam(r, "service_account_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidBillingInput, "service account not found")
		return
	}
	var req apiKeyRequest
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" || !validScopes(req.Scopes) {
		response.WriteError(w, 400, response.CodeInvalidBillingInput, "invalid request")
		return
	}
	key, secret, err := h.service.(developerService).CreateIntegrationAPIKeyForServiceAccount(r.Context(), serviceID, actor, req.input())
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "failed to create api key")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	_ = response.WriteJSON(w, 201, createdKeyResponse(key, secret))
}

func (h *Handler) RevokeDeveloperAPIKey(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "key_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidBillingInput, "key not found")
		return
	}
	if err = h.service.(developerService).RevokeIntegrationAPIKey(r.Context(), id, actor); err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "failed to revoke key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) RotateDeveloperAPIKey(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, 401, response.CodeUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "key_uuid"))
	if err != nil {
		response.WriteError(w, 404, response.CodeInvalidBillingInput, "key not found")
		return
	}
	var req struct {
		OverlapSeconds int64 `json:"overlap_seconds"`
	}
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || req.OverlapSeconds < 0 || req.OverlapSeconds > 86400 {
		response.WriteError(w, 400, response.CodeInvalidBillingInput, "invalid overlap")
		return
	}
	key, secret, err := h.service.(developerService).RotateIntegrationAPIKey(r.Context(), id, actor, time.Duration(req.OverlapSeconds)*time.Second)
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "failed to rotate key")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	_ = response.WriteJSON(w, 201, createdKeyResponse(key, secret))
}

func (h *Handler) ValidateSandboxKey(w http.ResponseWriter, r *http.Request) {
	h.validateIntegrationKey(w, r, "sandbox")
}
func (h *Handler) ValidateProductionKey(w http.ResponseWriter, r *http.Request) {
	h.validateIntegrationKey(w, r, "production")
}
func (h *Handler) validateIntegrationKey(w http.ResponseWriter, r *http.Request, environment string) {
	key := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if key == "" {
		key = strings.TrimSpace(r.Header.Get("X-API-Key"))
	}
	principal, err := h.service.(developerService).AuthenticateIntegrationKey(r.Context(), key, environment, "usage:read")
	if err != nil {
		switch {
		case errors.Is(err, models.ErrAPIKeyEnvironmentMismatch):
			response.WriteError(w, http.StatusUnauthorized, response.CodeKeyEnvironmentMismatch, "key environment mismatch")
		case errors.Is(err, models.ErrAPIKeyScopeDenied):
			response.WriteError(w, http.StatusForbidden, response.CodeAPIKeyScopeDenied, "api key scope denied")
		default:
			response.WriteError(w, http.StatusUnauthorized, response.CodeInvalidAPIKey, "invalid api key")
		}
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"application_uuid": principal.ApplicationUUID, "environment": principal.Environment, "scopes": principal.Scopes, "authenticated": true})
}

type developerApplicationRequest struct {
	OwnerType              string    `json:"owner_type"`
	OwnerUUID              uuid.UUID `json:"owner_uuid"`
	Name                   string    `json:"name"`
	Environment            string    `json:"environment"`
	Capabilities           []string  `json:"capabilities"`
	DailyCreditLimit       *int64    `json:"daily_credit_limit"`
	MonthlyCreditLimit     *int64    `json:"monthly_credit_limit"`
	MaxCreditsPerOperation *int64    `json:"max_credits_per_operation"`
}
type apiKeyRequest struct {
	Name                   string     `json:"name"`
	Scopes                 []string   `json:"scopes"`
	ExpiresAt              *time.Time `json:"expires_at"`
	PermanentCreditLimit   *int64     `json:"permanent_credit_limit"`
	TemporaryCreditLimit   *int64     `json:"temporary_credit_limit"`
	TemporaryLimitStartsAt *time.Time `json:"temporary_limit_starts_at"`
	TemporaryLimitEndsAt   *time.Time `json:"temporary_limit_ends_at"`
}

func (r apiKeyRequest) input() models.CreateIntegrationAPIKeyInput {
	return models.CreateIntegrationAPIKeyInput{Name: r.Name, Scopes: r.Scopes, ExpiresAt: r.ExpiresAt, PermanentCreditLimit: r.PermanentCreditLimit, TemporaryCreditLimit: r.TemporaryCreditLimit, TemporaryLimitStartsAt: r.TemporaryLimitStartsAt, TemporaryLimitEndsAt: r.TemporaryLimitEndsAt}
}

func (h *Handler) CreateDeveloperApplication(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	var req developerApplicationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid request")
		return
	}
	if req.OwnerType == "user" && req.OwnerUUID == uuid.Nil {
		req.OwnerUUID = actor
	}
	if !validCapabilities(req.Capabilities) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid capabilities")
		return
	}
	app, err := h.service.(developerService).CreateDeveloperApplication(r.Context(), models.CreateDeveloperApplicationInput{OwnerType: req.OwnerType, OwnerUUID: req.OwnerUUID, CreatedByUserUUID: actor, Name: req.Name, Environment: req.Environment, Capabilities: req.Capabilities, DailyCreditLimit: req.DailyCreditLimit, MonthlyCreditLimit: req.MonthlyCreditLimit, MaxCreditsPerOperation: req.MaxCreditsPerOperation})
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "failed to create application")
		return
	}
	_ = response.WriteJSON(w, http.StatusCreated, developerApplicationResponse(app))
}

func (h *Handler) ListDeveloperApplications(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	ownerType := r.URL.Query().Get("owner_type")
	ownerID := actor
	if ownerType == "" {
		ownerType = "user"
	}
	if raw := r.URL.Query().Get("owner_uuid"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid owner uuid")
			return
		}
		ownerID = parsed
	}
	apps, err := h.service.(developerService).ListDeveloperApplications(r.Context(), ownerType, ownerID, actor)
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "failed to list applications")
		return
	}
	items := make([]map[string]any, 0, len(apps))
	for _, app := range apps {
		items = append(items, developerApplicationResponse(app))
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"applications": items})
}

func (h *Handler) CreateDeveloperAPIKey(w http.ResponseWriter, r *http.Request) {
	actor, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	appID, err := uuid.Parse(chi.URLParam(r, "application_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid application uuid")
		return
	}
	var req apiKeyRequest
	if err = json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" || !validScopes(req.Scopes) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid request")
		return
	}
	key, secret, err := h.service.(developerService).CreateIntegrationAPIKey(r.Context(), appID, actor, req.input())
	if err != nil {
		writeBillingError(w, err, response.CodeInvalidBillingInput, "failed to create api key")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	_ = response.WriteJSON(w, http.StatusCreated, createdKeyResponse(key, secret))
}

func createdKeyResponse(key models.IntegrationAPIKey, secret string) map[string]any {
	return map[string]any{"key_uuid": key.ID, "service_account_uuid": key.ServiceAccountID, "name": key.Name, "prefix": key.Prefix, "scopes": key.Scopes, "expires_at": key.ExpiresAt, "permanent_credit_limit": key.PermanentCreditLimit, "temporary_credit_limit": key.TemporaryCreditLimit, "temporary_limit_starts_at": key.TemporaryLimitStartsAt, "temporary_limit_ends_at": key.TemporaryLimitEndsAt, "created_at": key.CreatedAt, "secret": secret, "secret_visible_once": true}
}

func developerApplicationResponse(app models.DeveloperApplication) map[string]any {
	return map[string]any{"application_uuid": app.ID, "owner_type": app.OwnerType, "owner_uuid": func() uuid.UUID {
		if app.UserUUID.Valid {
			return app.UserUUID.UUID
		}
		return app.CompanyUUID.UUID
	}(), "name": app.Name, "environment": app.Environment, "status": app.Status, "capabilities": app.Capabilities, "daily_credit_limit": app.DailyCreditLimit, "monthly_credit_limit": app.MonthlyCreditLimit, "max_credits_per_operation": app.MaxCreditsPerOperation, "lock_version": app.LockVersion, "created_at": app.CreatedAt, "updated_at": app.UpdatedAt}
}
func validCapabilities(values []string) bool {
	allowed := map[string]bool{"calls:write": true, "calls:read": true, "usage:read": true, "destinations:read": true, "exports:write": true, "exports:read": true, "webhooks:manage": true, "webhooks:read": true, "webhooks:write": true, "ai:real": true}
	for _, v := range values {
		if !allowed[v] {
			return false
		}
	}
	return true
}
func validScopes(values []string) bool {
	if len(values) == 0 {
		return false
	}
	return validCapabilities(values)
}
