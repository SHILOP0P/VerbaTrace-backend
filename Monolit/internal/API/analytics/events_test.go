package analytics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type deepAnalysisEventsService struct {
	analyses []models.AggregateAnalysis
	index    int
}

func (s *deepAnalysisEventsService) GetOverview(context.Context, models.AnalyticsOverviewInput) (models.AnalyticsOverview, error) {
	panic("not implemented")
}

func (s *deepAnalysisEventsService) CreateDeepAnalysis(context.Context, models.CreateDeepAnalysisInput) (models.AggregateAnalysis, error) {
	panic("not implemented")
}

func (s *deepAnalysisEventsService) ListDeepAnalyses(context.Context, models.ListDeepAnalysesInput) (models.ListAggregateAnalysesResult, error) {
	panic("not implemented")
}

func (s *deepAnalysisEventsService) GetDeepAnalysis(context.Context, uuid.UUID, uuid.UUID) (models.AggregateAnalysis, error) {
	analysis := s.analyses[s.index]
	if s.index < len(s.analyses)-1 {
		s.index++
	}
	return analysis, nil
}

func (s *deepAnalysisEventsService) CreateAggregateReport(context.Context, models.CreateAggregateReportInput) (models.AggregateReportExport, error) {
	panic("not implemented")
}

func (s *deepAnalysisEventsService) ListAggregateReports(context.Context, uuid.UUID, uuid.UUID) ([]models.AggregateReportExport, error) {
	panic("not implemented")
}

func (s *deepAnalysisEventsService) GetAggregateReportFile(context.Context, uuid.UUID, uuid.UUID) (models.AggregateReportFile, error) {
	panic("not implemented")
}

func (s *deepAnalysisEventsService) DeleteAggregateReport(context.Context, uuid.UUID, uuid.UUID) error {
	panic("not implemented")
}

func TestDeepAnalysisEventsWritesCurrentTerminalStatus(t *testing.T) {
	analysisID := uuid.New()
	userID := uuid.New()
	handler := NewHandler(&deepAnalysisEventsService{analyses: []models.AggregateAnalysis{{ID: analysisID, Status: models.AggregateAnalysisStatusDone}}})

	rec, req := deepAnalysisEventsRequest(analysisID, userID)
	handler.DeepAnalysisEvents(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Body.String(), "event: status")
	require.Contains(t, rec.Body.String(), `"status":"done"`)
	require.Contains(t, rec.Body.String(), `"terminal":true`)
	require.True(t, rec.Flushed)
}

func TestDeepAnalysisEventsStreamsStatusChangesUntilTerminalStatus(t *testing.T) {
	analysisID := uuid.New()
	userID := uuid.New()
	original := deepAnalysisEventsPollInterval
	deepAnalysisEventsPollInterval = time.Millisecond
	defer func() { deepAnalysisEventsPollInterval = original }()

	handler := NewHandler(&deepAnalysisEventsService{analyses: []models.AggregateAnalysis{
		{ID: analysisID, Status: models.AggregateAnalysisStatusProcessing},
		{ID: analysisID, Status: models.AggregateAnalysisStatusDone},
	}})

	rec, req := deepAnalysisEventsRequest(analysisID, userID)
	handler.DeepAnalysisEvents(rec, req)

	body := rec.Body.String()
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 2, strings.Count(body, "event: status"))
	require.Contains(t, body, `"status":"processing"`)
	require.Contains(t, body, `"status":"done"`)
	require.Contains(t, body, `"terminal":false`)
	require.Contains(t, body, `"terminal":true`)
}

func deepAnalysisEventsRequest(analysisID uuid.UUID, userID uuid.UUID) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/deep-analyses/"+analysisID.String()+"/events", nil)
	req = req.WithContext(middleware.ContextWithUserID(req.Context(), userID))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("uuid", analysisID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
	return httptest.NewRecorder(), req
}
