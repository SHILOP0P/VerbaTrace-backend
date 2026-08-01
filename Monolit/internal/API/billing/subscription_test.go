package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestActivateCompanySubscriptionSuccess(t *testing.T) {
	companyID := uuid.New()
	userID := uuid.New()
	subscription := testSubscription(companyID, models.PlanCodeBusinessPlus, models.SubscriptionStatusActive)
	fake := &fakeBillingService{}
	fake.activate = func(ctx context.Context, input models.ActivateCompanySubscriptionInput) (models.Subscription, error) {
		require.Equal(t, companyID, input.CompanyUUID)
		require.Equal(t, userID, input.RequestUser)
		require.Equal(t, models.PlanCodeBusinessPlus, input.PlanCode)
		return subscription, nil
	}
	api := NewHandler(fake)

	rec, req := billingRequest(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/subscription/activate", `{"plan_code":"business_plus"}`, userID, map[string]string{"uuid": companyID.String()})
	api.ActivateCompanySubscription(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var respBody struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Plan   struct {
			Code string `json:"code"`
		} `json:"plan"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &respBody))
	require.Equal(t, subscription.ID.String(), respBody.ID)
	require.Equal(t, string(models.SubscriptionStatusActive), respBody.Status)
	require.Equal(t, string(models.PlanCodeBusinessPlus), respBody.Plan.Code)
}

func TestActivatePersonalSubscriptionSuccess(t *testing.T) {
	userID := uuid.New()
	subscription := testPersonalSubscription(userID, models.PlanCodePersonalPlus, models.SubscriptionStatusActive)
	fake := &fakeBillingService{}
	fake.activatePersonal = func(ctx context.Context, input models.ActivatePersonalSubscriptionInput) (models.Subscription, error) {
		require.Equal(t, userID, input.UserUUID)
		require.Equal(t, models.PlanCodePersonalPlus, input.PlanCode)
		return subscription, nil
	}
	api := NewHandler(fake)

	rec, req := billingRequest(http.MethodPost, "/api/v1/subscription/activate", `{"plan_code":"personal_plus"}`, userID, nil)
	api.ActivatePersonalSubscription(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var respBody struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Plan   struct {
			Code string `json:"code"`
		} `json:"plan"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &respBody))
	require.Equal(t, subscription.ID.String(), respBody.ID)
	require.Equal(t, string(models.SubscriptionStatusActive), respBody.Status)
	require.Equal(t, string(models.PlanCodePersonalPlus), respBody.Plan.Code)
}

func TestGetPersonalSubscriptionSuccess(t *testing.T) {
	userID := uuid.New()
	subscription := testPersonalSubscription(userID, models.PlanCodePersonalPro, models.SubscriptionStatusActive)
	fake := &fakeBillingService{}
	fake.getPersonal = func(ctx context.Context, id uuid.UUID) (models.Subscription, error) {
		require.Equal(t, userID, id)
		return subscription, nil
	}
	api := NewHandler(fake)

	rec, req := billingRequest(http.MethodGet, "/api/v1/subscription", "", userID, nil)
	api.GetPersonalSubscription(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestGetPersonalSubscriptionUsageSuccess(t *testing.T) {
	userID := uuid.New()
	subscription := testPersonalSubscription(userID, models.PlanCodePersonalPro, models.SubscriptionStatusActive)
	fake := &fakeBillingService{}
	fake.getPersonalUsage = func(ctx context.Context, input models.GetPersonalSubscriptionUsageInput) (models.SubscriptionUsage, error) {
		require.Equal(t, userID, input.UserUUID)
		require.NotNil(t, input.PeriodStart)
		require.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), *input.PeriodStart)
		return models.SubscriptionUsage{
			Subscription:     subscription,
			PeriodStart:      *input.PeriodStart,
			PeriodEnd:        input.PeriodStart.AddDate(0, 1, 0),
			UsedMinutes:      86,
			LimitMinutes:     7200,
			RemainingMinutes: 7114,
			Percent:          1.19,
		}, nil
	}
	api := NewHandler(fake)

	rec, req := billingRequest(http.MethodGet, "/api/v1/subscription/usage?period=2026-07", "", userID, nil)
	api.GetPersonalSubscriptionUsage(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var respBody struct {
		UsedMinutes      int     `json:"used_minutes"`
		LimitMinutes     int     `json:"limit_minutes"`
		RemainingMinutes int     `json:"remaining_minutes"`
		Percent          float64 `json:"percent"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &respBody))
	require.Equal(t, 86, respBody.UsedMinutes)
	require.Equal(t, 7200, respBody.LimitMinutes)
	require.Equal(t, 7114, respBody.RemainingMinutes)
	require.Equal(t, 1.19, respBody.Percent)
}

func TestGetCompanySubscriptionUsageSuccess(t *testing.T) {
	companyID := uuid.New()
	userID := uuid.New()
	subscription := testSubscription(companyID, models.PlanCodeBusinessPlus, models.SubscriptionStatusActive)
	membersLimit := 15
	membersUsed := 8
	departmentsLimit := 5
	departmentsUsed := 2
	instructionsLimit := 10
	instructionsUsed := 4
	fake := &fakeBillingService{}
	fake.getCompanyUsage = func(ctx context.Context, input models.GetCompanySubscriptionUsageInput) (models.SubscriptionUsage, error) {
		require.Equal(t, companyID, input.CompanyUUID)
		require.Equal(t, userID, input.RequestUser)
		require.Nil(t, input.PeriodStart)
		now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		return models.SubscriptionUsage{
			Subscription:            subscription,
			PeriodStart:             now,
			PeriodEnd:               now.AddDate(0, 1, 0),
			UsedMinutes:             86,
			LimitMinutes:            7200,
			RemainingMinutes:        7114,
			Percent:                 1.19,
			MembersLimit:            &membersLimit,
			MembersUsed:             &membersUsed,
			DepartmentsLimit:        &departmentsLimit,
			DepartmentsUsed:         &departmentsUsed,
			ActiveInstructionsLimit: &instructionsLimit,
			ActiveInstructionsUsed:  &instructionsUsed,
		}, nil
	}
	api := NewHandler(fake)

	rec, req := billingRequest(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/subscription/usage", "", userID, map[string]string{"uuid": companyID.String()})
	api.GetCompanySubscriptionUsage(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var respBody struct {
		MembersLimit            int `json:"members_limit"`
		MembersUsed             int `json:"members_used"`
		DepartmentsLimit        int `json:"departments_limit"`
		DepartmentsUsed         int `json:"departments_used"`
		ActiveInstructionsLimit int `json:"active_instructions_limit"`
		ActiveInstructionsUsed  int `json:"active_instructions_used"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &respBody))
	require.Equal(t, 15, respBody.MembersLimit)
	require.Equal(t, 8, respBody.MembersUsed)
	require.Equal(t, 5, respBody.DepartmentsLimit)
	require.Equal(t, 2, respBody.DepartmentsUsed)
	require.Equal(t, 10, respBody.ActiveInstructionsLimit)
	require.Equal(t, 4, respBody.ActiveInstructionsUsed)
}

func TestGetPersonalSubscriptionUsageRequiresAuth(t *testing.T) {
	api := NewHandler(&fakeBillingService{})

	rec, req := billingRequest(http.MethodGet, "/api/v1/subscription/usage", "", uuid.Nil, nil)
	api.GetPersonalSubscriptionUsage(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	requireBillingErrorCode(t, rec, response.CodeUnauthorized)
}

func TestGetPersonalSubscriptionUsageRejectsInvalidPeriod(t *testing.T) {
	api := NewHandler(&fakeBillingService{})

	rec, req := billingRequest(http.MethodGet, "/api/v1/subscription/usage?period=2026-7", "", uuid.New(), nil)
	api.GetPersonalSubscriptionUsage(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	requireBillingErrorCode(t, rec, response.CodeInvalidBillingInput)
}

func TestGetCompanySubscriptionUsageMapsForbidden(t *testing.T) {
	companyID := uuid.New()
	userID := uuid.New()
	fake := &fakeBillingService{}
	fake.getCompanyUsage = func(ctx context.Context, input models.GetCompanySubscriptionUsageInput) (models.SubscriptionUsage, error) {
		require.Equal(t, companyID, input.CompanyUUID)
		require.Equal(t, userID, input.RequestUser)
		return models.SubscriptionUsage{}, models.ErrForbidden
	}
	api := NewHandler(fake)

	rec, req := billingRequest(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/subscription/usage", "", userID, map[string]string{"uuid": companyID.String()})
	api.GetCompanySubscriptionUsage(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	requireBillingErrorCode(t, rec, response.CodeForbidden)
}

func TestGetCompanySubscriptionUsageMapsNotFound(t *testing.T) {
	companyID := uuid.New()
	userID := uuid.New()
	fake := &fakeBillingService{}
	fake.getCompanyUsage = func(ctx context.Context, input models.GetCompanySubscriptionUsageInput) (models.SubscriptionUsage, error) {
		require.Equal(t, companyID, input.CompanyUUID)
		require.Equal(t, userID, input.RequestUser)
		return models.SubscriptionUsage{}, models.ErrSubscriptionNotFound
	}
	api := NewHandler(fake)

	rec, req := billingRequest(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/subscription/usage", "", userID, map[string]string{"uuid": companyID.String()})
	api.GetCompanySubscriptionUsage(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	requireBillingErrorCode(t, rec, response.CodeSubscriptionNotFound)
}

func TestGetCompanySubscriptionMapsNotFound(t *testing.T) {
	companyID := uuid.New()
	userID := uuid.New()
	fake := &fakeBillingService{}
	fake.getCompany = func(ctx context.Context, input models.GetCompanySubscriptionInput) (models.Subscription, error) {
		require.Equal(t, companyID, input.CompanyUUID)
		require.Equal(t, userID, input.RequestUser)
		return models.Subscription{}, models.ErrSubscriptionNotFound
	}
	api := NewHandler(fake)

	rec, req := billingRequest(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/subscription", "", userID, map[string]string{"uuid": companyID.String()})
	api.GetCompanySubscription(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	requireBillingErrorCode(t, rec, response.CodeSubscriptionNotFound)
}

func TestActivateCompanySubscriptionRequiresAuth(t *testing.T) {
	companyID := uuid.New()
	api := NewHandler(&fakeBillingService{})

	rec, req := billingRequest(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/subscription/activate", "", uuid.Nil, map[string]string{"uuid": companyID.String()})
	api.ActivateCompanySubscription(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	requireBillingErrorCode(t, rec, response.CodeUnauthorized)
}

func TestActivateCompanySubscriptionRejectsInvalidBody(t *testing.T) {
	companyID := uuid.New()
	api := NewHandler(&fakeBillingService{})

	rec, req := billingRequest(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/subscription/activate", "{", uuid.New(), map[string]string{"uuid": companyID.String()})
	api.ActivateCompanySubscription(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	requireBillingErrorCode(t, rec, response.CodeInvalidRequestBody)
}

func TestCancelCompanySubscriptionMapsNotFound(t *testing.T) {
	companyID := uuid.New()
	userID := uuid.New()
	fake := &fakeBillingService{}
	fake.cancel = func(ctx context.Context, input models.CancelCompanySubscriptionInput) (models.Subscription, error) {
		require.Equal(t, companyID, input.CompanyUUID)
		require.Equal(t, userID, input.RequestUser)
		return models.Subscription{}, models.ErrSubscriptionNotFound
	}
	api := NewHandler(fake)

	rec, req := billingRequest(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/subscription/cancel", "", userID, map[string]string{"uuid": companyID.String()})
	api.CancelCompanySubscription(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	requireBillingErrorCode(t, rec, response.CodeSubscriptionNotFound)
}

func billingRequest(method string, path string, body string, userID uuid.UUID, params map[string]string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if userID != uuid.Nil {
		req = req.WithContext(middleware.ContextWithUserID(req.Context(), userID))
	}
	if len(params) > 0 {
		routeCtx := chi.NewRouteContext()
		for key, value := range params {
			routeCtx.URLParams.Add(key, value)
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	}
	return httptest.NewRecorder(), req
}

func requireBillingErrorCode(t *testing.T, rec *httptest.ResponseRecorder, expectedCode string) {
	var resp response.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, expectedCode, resp.Error.Code)
}

func testSubscription(companyID uuid.UUID, planCode models.PlanCode, status models.SubscriptionStatus) models.Subscription {
	now := time.Now().UTC()
	return models.Subscription{
		ID:          uuid.New(),
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
		Status:      status,
		StartsAt:    now,
		CreatedAt:   now,
		UpdatedAt:   now,
		Plan: models.Plan{
			ID:                     uuid.New(),
			Code:                   planCode,
			Type:                   models.PlanTypeBusiness,
			Name:                   "Business",
			MonthlyMinutesLimit:    1000,
			ActiveInstructionLimit: 0,
			AnalysisLevel:          models.AnalysisLevelPlus,
			CreatedAt:              now,
			UpdatedAt:              now,
		},
	}
}

func testPersonalSubscription(userID uuid.UUID, planCode models.PlanCode, status models.SubscriptionStatus) models.Subscription {
	subscription := testSubscription(uuid.New(), planCode, status)
	subscription.UserUUID = uuid.NullUUID{UUID: userID, Valid: true}
	subscription.CompanyUUID = uuid.NullUUID{}
	subscription.Plan.Type = models.PlanTypePersonal
	subscription.Plan.Name = "Personal"
	return subscription
}

type fakeBillingService struct {
	list             func(context.Context) ([]models.Plan, error)
	getPersonal      func(context.Context, uuid.UUID) (models.Subscription, error)
	getCompany       func(context.Context, models.GetCompanySubscriptionInput) (models.Subscription, error)
	getPersonalUsage func(context.Context, models.GetPersonalSubscriptionUsageInput) (models.SubscriptionUsage, error)
	getCompanyUsage  func(context.Context, models.GetCompanySubscriptionUsageInput) (models.SubscriptionUsage, error)
	activatePersonal func(context.Context, models.ActivatePersonalSubscriptionInput) (models.Subscription, error)
	activate         func(context.Context, models.ActivateCompanySubscriptionInput) (models.Subscription, error)
	cancel           func(context.Context, models.CancelCompanySubscriptionInput) (models.Subscription, error)
}

func (f *fakeBillingService) ListPlans(ctx context.Context) ([]models.Plan, error) {
	return f.list(ctx)
}

func (f *fakeBillingService) GetPersonalSubscription(ctx context.Context, userID uuid.UUID) (models.Subscription, error) {
	return f.getPersonal(ctx, userID)
}

func (f *fakeBillingService) GetCompanySubscription(ctx context.Context, input models.GetCompanySubscriptionInput) (models.Subscription, error) {
	return f.getCompany(ctx, input)
}

func (f *fakeBillingService) GetPersonalSubscriptionUsage(ctx context.Context, input models.GetPersonalSubscriptionUsageInput) (models.SubscriptionUsage, error) {
	return f.getPersonalUsage(ctx, input)
}

func (f *fakeBillingService) GetCompanySubscriptionUsage(ctx context.Context, input models.GetCompanySubscriptionUsageInput) (models.SubscriptionUsage, error) {
	return f.getCompanyUsage(ctx, input)
}

func (f *fakeBillingService) ActivateCompanySubscription(ctx context.Context, input models.ActivateCompanySubscriptionInput) (models.Subscription, error) {
	return f.activate(ctx, input)
}

func (f *fakeBillingService) ActivatePersonalSubscription(ctx context.Context, input models.ActivatePersonalSubscriptionInput) (models.Subscription, error) {
	return f.activatePersonal(ctx, input)
}

func (f *fakeBillingService) CancelCompanySubscription(ctx context.Context, input models.CancelCompanySubscriptionInput) (models.Subscription, error) {
	return f.cancel(ctx, input)
}
