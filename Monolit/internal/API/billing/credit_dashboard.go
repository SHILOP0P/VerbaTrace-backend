package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type creditDashboardService interface {
	GetPersonalCreditDashboard(context.Context, uuid.UUID, time.Time, time.Time) (models.CreditDashboard, error)
	GetCompanyCreditDashboard(context.Context, uuid.UUID, uuid.UUID, time.Time, time.Time) (models.CreditDashboard, error)
	UpdateCompanyCreditVisibility(context.Context, models.UpdateCompanyCreditVisibilityInput) (models.CreditDashboard, error)
}

type companyCreditVisibilityRequest struct {
	Visible bool `json:"visible"`
}

func (h *Handler) GetPersonalCreditDashboard(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	h.getCreditDashboard(w, r, userID, uuid.Nil)
}

func (h *Handler) GetCompanyCreditDashboard(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid company uuid")
		return
	}
	h.getCreditDashboard(w, r, userID, companyID)
}

func (h *Handler) UpdateCompanyCreditVisibility(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid company uuid")
		return
	}
	var request companyCreditVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid request body")
		return
	}
	service, ok := h.service.(creditDashboardService)
	if !ok {
		response.WriteError(w, http.StatusNotImplemented, response.CodeInvalidBillingInput, "credit dashboard unavailable")
		return
	}
	dashboard, err := service.UpdateCompanyCreditVisibility(r.Context(), models.UpdateCompanyCreditVisibilityInput{CompanyUUID: companyID, RequestUser: userID, Visible: request.Visible})
	if err != nil {
		writeBillingError(w, err, response.CodeFailedToGetCreditDashboard, "failed to update credit dashboard visibility")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"visible_to_members": dashboard.VisibleToMembers, "can_manage_visibility": dashboard.CanManageVisibility})
}

func (h *Handler) getCreditDashboard(w http.ResponseWriter, r *http.Request, userID, companyID uuid.UUID) {
	to := time.Now().UTC().Add(24 * time.Hour).Truncate(24 * time.Hour)
	from := to.AddDate(0, 0, -364)
	if value := r.URL.Query().Get("from"); value != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid from date")
			return
		}
		from = parsed
	}
	if value := r.URL.Query().Get("to"); value != "" {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid to date")
			return
		}
		to = parsed.Add(24 * time.Hour)
	}
	service, ok := h.service.(creditDashboardService)
	if !ok {
		response.WriteError(w, http.StatusNotImplemented, response.CodeInvalidBillingInput, "credit dashboard unavailable")
		return
	}
	var dashboard models.CreditDashboard
	var err error
	if companyID == uuid.Nil {
		dashboard, err = service.GetPersonalCreditDashboard(r.Context(), userID, from, to)
	} else {
		dashboard, err = service.GetCompanyCreditDashboard(r.Context(), companyID, userID, from, to)
	}
	if err != nil {
		writeBillingError(w, err, response.CodeFailedToGetCreditDashboard, "failed to get credit dashboard")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, dashboardResponse(dashboard))
}

func dashboardResponse(value models.CreditDashboard) map[string]any {
	activity := make([]map[string]any, 0, len(value.Activity))
	for _, day := range value.Activity {
		activity = append(activity, map[string]any{"date": day.Date.Format("2006-01-02"), "credits": day.Credits, "transcription": day.Transcription, "analysis": day.Analysis, "calls": day.Calls})
	}
	entries := make([]map[string]any, 0, len(value.WalletEntries))
	for _, item := range value.WalletEntries {
		entries = append(entries, map[string]any{"transaction_uuid": item.TransactionUUID, "type": item.Type, "credits": item.Credits, "reason": item.Reason, "created_at": item.CreatedAt})
	}
	return map[string]any{"allowance_credits": value.AllowanceCredits, "allowance_remaining": value.AllowanceRemaining, "allowance_remaining_percent": value.RemainingPercent, "days_until_reset": value.DaysUntilReset, "resets_at": value.ResetsAt, "allowance_exhausted": value.AllowanceExhausted, "wallet_credits": value.WalletCredits, "activity": activity, "wallet_entries": entries, "visible_to_members": value.VisibleToMembers, "can_manage_visibility": value.CanManageVisibility, "calls_awaiting_credits": value.CallsAwaitingCredits, "pending_credit_calls_limit": value.PendingCreditCallsLimit}
}
