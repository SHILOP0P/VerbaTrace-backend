package billing

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"
	serviceMocks "verbatrace/monolit/internal/service/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestListPlansWithMockery(t *testing.T) {
	service := serviceMocks.NewBillingService(t)
	handler := NewHandler(service)
	service.EXPECT().ListPlans(mock.Anything).Return([]models.Plan{{
		ID: uuid.New(), Code: models.PlanCodePersonalStart, Type: models.PlanTypePersonal,
	}}, nil).Once()
	rec := httptest.NewRecorder()
	handler.ListPlans(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	service.EXPECT().ListPlans(mock.Anything).Return(nil, errors.New("db")).Once()
	rec = httptest.NewRecorder()
	handler.ListPlans(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("error status = %d", rec.Code)
	}
}

func TestBillingValidationWithMockery(t *testing.T) {
	service := serviceMocks.NewBillingService(t)
	handler := NewHandler(service)

	for _, method := range []func(http.ResponseWriter, *http.Request){
		handler.GetPersonalSubscription,
		handler.GetCompanySubscription,
		handler.ActivatePersonalSubscription,
		handler.ActivateCompanySubscription,
		handler.CancelCompanySubscription,
	} {
		rec, req := billingRequest(http.MethodPost, "/", "", uuid.Nil, nil)
		method(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized status = %d", rec.Code)
		}
	}

	userID := uuid.New()
	for _, method := range []func(http.ResponseWriter, *http.Request){
		handler.GetCompanySubscription,
		handler.ActivateCompanySubscription,
		handler.CancelCompanySubscription,
	} {
		rec, req := billingRequest(http.MethodPost, "/", "{}", userID, map[string]string{"uuid": "bad"})
		method(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid company status = %d", rec.Code)
		}
	}

	rec, req := billingRequest(http.MethodPost, "/", "{", userID, nil)
	handler.ActivatePersonalSubscription(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid personal body status = %d", rec.Code)
	}

	emptyReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	if decoded, err := decodeActivateSubscriptionRequest(emptyReq); err != nil || decoded.PlanCode != "" {
		t.Fatalf("empty request = %+v, %v", decoded, err)
	}
}

func TestWriteBillingErrorMappings(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code int
	}{
		{models.ErrInvalidBillingInput, http.StatusBadRequest},
		{models.ErrPlanNotFound, http.StatusBadRequest},
		{models.ErrCompanyNotFound, http.StatusNotFound},
		{models.ErrForbidden, http.StatusForbidden},
		{models.ErrAPIKeyScopeDenied, http.StatusForbidden},
		{models.ErrSubscriptionNotFound, http.StatusNotFound},
		{errors.New("db"), http.StatusInternalServerError},
	} {
		rec := httptest.NewRecorder()
		writeBillingError(rec, tt.err, response.CodeInternalServerError, "failed")
		if rec.Code != tt.code {
			t.Fatalf("error %v: status=%d want=%d", tt.err, rec.Code, tt.code)
		}
	}
}
