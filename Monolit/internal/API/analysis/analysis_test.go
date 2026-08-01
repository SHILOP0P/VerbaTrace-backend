package analysis

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	serviceMocks "verbatrace/monolit/internal/service/mocks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestHandlers(t *testing.T) {
	userID := uuid.New()
	callID := uuid.New()
	result := models.CallAnalysis{
		ID: uuid.New(), CallUUID: callID, Status: models.CallAnalysisStatusPending,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	service := serviceMocks.NewAnalysisService(t)
	service.EXPECT().AnalyzeCall(mock.Anything, mock.Anything).Return(result, nil).Once()
	service.EXPECT().GetByCallUUID(mock.Anything, mock.Anything, mock.Anything).Return(result, nil).Once()
	handler := NewHandler(service)

	for _, method := range []func(http.ResponseWriter, *http.Request){handler.AnalyzeCall, handler.GetByCallUUID} {
		rec, req := analysisRequest(userID, callID.String())
		method(rec, req)
		if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}

		rec, req = analysisRequest(uuid.Nil, callID.String())
		method(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized status = %d", rec.Code)
		}

		rec, req = analysisRequest(userID, "bad")
		method(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad UUID status = %d", rec.Code)
		}
	}
}

func TestHandlerErrorMappings(t *testing.T) {
	userID := uuid.New()
	callID := uuid.New()
	analyzeErrors := []struct {
		err  error
		code int
	}{
		{models.ErrCallNotFound, http.StatusNotFound},
		{models.ErrTranscriptionNotFound, http.StatusNotFound},
		{models.ErrInvalidAnalysisInput, http.StatusBadRequest},
		{models.ErrAnalyzerNotConfigured, http.StatusServiceUnavailable},
		{models.ErrInvalidAnalysisStatus, http.StatusConflict},
		{errors.New("db"), http.StatusInternalServerError},
	}
	for _, tt := range analyzeErrors {
		service := serviceMocks.NewAnalysisService(t)
		service.EXPECT().AnalyzeCall(mock.Anything, mock.Anything).Return(models.CallAnalysis{}, tt.err).Once()
		handler := NewHandler(service)
		rec, req := analysisRequest(userID, callID.String())
		handler.AnalyzeCall(rec, req)
		if rec.Code != tt.code {
			t.Fatalf("AnalyzeCall error %v: status=%d want=%d", tt.err, rec.Code, tt.code)
		}
	}

	getErrors := []struct {
		err  error
		code int
	}{
		{models.ErrCallNotFound, http.StatusNotFound},
		{models.ErrAnalysisNotFound, http.StatusNotFound},
		{models.ErrInvalidAnalysisInput, http.StatusBadRequest},
		{errors.New("db"), http.StatusInternalServerError},
	}
	for _, tt := range getErrors {
		service := serviceMocks.NewAnalysisService(t)
		service.EXPECT().GetByCallUUID(mock.Anything, mock.Anything, mock.Anything).Return(models.CallAnalysis{}, tt.err).Once()
		handler := NewHandler(service)
		rec, req := analysisRequest(userID, callID.String())
		handler.GetByCallUUID(rec, req)
		if rec.Code != tt.code {
			t.Fatalf("GetByCallUUID error %v: status=%d want=%d", tt.err, rec.Code, tt.code)
		}
	}
}

func analysisRequest(userID uuid.UUID, rawID string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if userID != uuid.Nil {
		req = req.WithContext(middleware.ContextWithUserID(req.Context(), userID))
	}
	route := chi.NewRouteContext()
	route.URLParams.Add("uuid", rawID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
	return httptest.NewRecorder(), req
}
