package billing

import (
	"errors"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) GetPersonalSubscription(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	subscription, err := h.service.GetPersonalSubscription(r.Context(), requestUserID)
	if err != nil {
		writeBillingError(w, err, response.CodeSubscriptionNotFound, "subscription not found")
		return
	}

	writeSubscriptionResponse(w, subscription)
}

func (h *Handler) GetCompanySubscription(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, ok := companyIDFromRequest(w, r)
	if !ok {
		return
	}

	subscription, err := h.service.GetCompanySubscription(r.Context(), models.GetCompanySubscriptionInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
	})
	if err != nil {
		writeBillingError(w, err, response.CodeSubscriptionNotFound, "subscription not found")
		return
	}

	writeSubscriptionResponse(w, subscription)
}

func (h *Handler) GetPersonalSubscriptionUsage(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	periodStart, ok := periodFromRequest(w, r)
	if !ok {
		return
	}

	usage, err := h.service.GetPersonalSubscriptionUsage(r.Context(), models.GetPersonalSubscriptionUsageInput{
		UserUUID:    requestUserID,
		PeriodStart: periodStart,
	})
	if err != nil {
		writeBillingError(w, err, response.CodeSubscriptionNotFound, "subscription not found")
		return
	}

	writeSubscriptionUsageResponse(w, usage)
}

func (h *Handler) GetCompanySubscriptionUsage(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, ok := companyIDFromRequest(w, r)
	if !ok {
		return
	}

	periodStart, ok := periodFromRequest(w, r)
	if !ok {
		return
	}

	usage, err := h.service.GetCompanySubscriptionUsage(r.Context(), models.GetCompanySubscriptionUsageInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		PeriodStart: periodStart,
	})
	if err != nil {
		writeBillingError(w, err, response.CodeSubscriptionNotFound, "subscription not found")
		return
	}

	writeSubscriptionUsageResponse(w, usage)
}

// CancelCompanySubscription stops the plan that covers this company. Buying one
// is an administrator's job for now, so there is no matching activation here.
func (h *Handler) CancelCompanySubscription(w http.ResponseWriter, r *http.Request) {
	requestUserID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, ok := companyIDFromRequest(w, r)
	if !ok {
		return
	}

	subscription, err := h.service.CancelCompanySubscription(r.Context(), models.CancelCompanySubscriptionInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
	})
	if err != nil {
		writeBillingError(w, err, response.CodeFailedToCancelSubscription, "failed to cancel subscription")
		return
	}

	writeSubscriptionResponse(w, subscription)
}

func companyIDFromRequest(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid company uuid")
		return uuid.Nil, false
	}

	return companyID, true
}

func periodFromRequest(w http.ResponseWriter, r *http.Request) (*time.Time, bool) {
	period := r.URL.Query().Get("period")
	if period == "" {
		return nil, true
	}

	parsed, err := time.Parse("2006-01", period)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid period")
		return nil, false
	}

	parsed = time.Date(parsed.Year(), parsed.Month(), 1, 0, 0, 0, 0, time.UTC)
	return &parsed, true
}

func writeSubscriptionResponse(w http.ResponseWriter, subscription models.Subscription) {
	resp, err := converter.SubscriptionModelToAPI(subscription)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertSubscription, "failed to convert subscription")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}

func writeSubscriptionUsageResponse(w http.ResponseWriter, usage models.SubscriptionUsage) {
	resp, err := converter.SubscriptionUsageModelToAPI(usage)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertSubscription, "failed to convert subscription")
		return
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}

func writeBillingError(w http.ResponseWriter, err error, fallbackCode string, fallbackMessage string) {
	if errors.Is(err, models.ErrInvalidBillingInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "invalid billing input")
		return
	}
	if errors.Is(err, models.ErrPlanNotFound) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidBillingInput, "plan not found")
		return
	}
	if errors.Is(err, models.ErrCompanyNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
		return
	}
	if errors.Is(err, models.ErrForbidden) {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
		return
	}
	if errors.Is(err, models.ErrAPIKeyScopeDenied) {
		response.WriteError(w, http.StatusForbidden, response.CodeAPIKeyScopeDenied, "requested scopes exceed application capabilities")
		return
	}
	if errors.Is(err, models.ErrSubscriptionNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeSubscriptionNotFound, "subscription not found")
		return
	}
	if errors.Is(err, models.ErrIntegrationConflict) {
		response.WriteError(w, http.StatusConflict, "integration_conflict", "resource was changed by another request")
		return
	}

	response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
}
