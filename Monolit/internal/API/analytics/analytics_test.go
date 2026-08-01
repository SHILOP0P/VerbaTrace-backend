package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeAnalyticsService struct {
	t     *testing.T
	check func(models.AnalyticsOverviewInput)
}

func (s fakeAnalyticsService) GetOverview(ctx context.Context, input models.AnalyticsOverviewInput) (models.AnalyticsOverview, error) {
	s.check(input)
	avgDuration := 438
	return models.AnalyticsOverview{
		CallsTotal:             31,
		CallsNew:               2,
		CallsProcessing:        1,
		CallsTranscribed:       8,
		CallsAnalyzed:          20,
		AverageDurationSeconds: &avgDuration,
		QualityScoreScale:      5,
		TopTopics:              []models.AnalyticsTopicCount{},
		Charts: models.AnalyticsCharts{
			CallsByDay: []models.AnalyticsCountPoint{{Date: "2026-07-01", Count: 4}},
		},
	}, nil
}

func (s fakeAnalyticsService) CreateDeepAnalysis(context.Context, models.CreateDeepAnalysisInput) (models.AggregateAnalysis, error) {
	panic("not implemented")
}

func (s fakeAnalyticsService) ListDeepAnalyses(context.Context, models.ListDeepAnalysesInput) (models.ListAggregateAnalysesResult, error) {
	panic("not implemented")
}

func (s fakeAnalyticsService) GetDeepAnalysis(context.Context, uuid.UUID, uuid.UUID) (models.AggregateAnalysis, error) {
	panic("not implemented")
}

func (s fakeAnalyticsService) CreateAggregateReport(context.Context, models.CreateAggregateReportInput) (models.AggregateReportExport, error) {
	panic("not implemented")
}

func (s fakeAnalyticsService) ListAggregateReports(context.Context, uuid.UUID, uuid.UUID) ([]models.AggregateReportExport, error) {
	panic("not implemented")
}

func (s fakeAnalyticsService) GetAggregateReportFile(context.Context, uuid.UUID, uuid.UUID) (models.AggregateReportFile, error) {
	panic("not implemented")
}

func (s fakeAnalyticsService) DeleteAggregateReport(context.Context, uuid.UUID, uuid.UUID) error {
	panic("not implemented")
}

func TestGetOverviewParsesFiltersAndReturnsNoUnsupportedMetrics(t *testing.T) {
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	handler := NewHandler(fakeAnalyticsService{
		t: t,
		check: func(input models.AnalyticsOverviewInput) {
			require.Equal(t, userID, input.UserID)
			require.Equal(t, models.CallVisibilityScopeDepartment, input.VisibilityScope)
			require.True(t, input.CompanyUUID.Valid)
			require.Equal(t, companyID, input.CompanyUUID.UUID)
			require.True(t, input.DepartmentUUID.Valid)
			require.Equal(t, departmentID, input.DepartmentUUID.UUID)
			require.NotNil(t, input.From)
			require.True(t, input.From.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)))
			require.NotNil(t, input.To)
			require.True(t, input.To.After(time.Date(2026, 7, 2, 23, 59, 59, 0, time.UTC)))
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/overview?scope=department&company_uuid="+companyID.String()+"&department_uuid="+departmentID.String()+"&from=2026-07-01&to=2026-07-02", nil)
	req = req.WithContext(middleware.ContextWithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	handler.GetOverview(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp dto.AnalyticsOverviewResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 31, resp.CallsTotal)
	require.Equal(t, 20, resp.CallsAnalyzed)
	require.NotNil(t, resp.AverageDurationSeconds)
	require.Nil(t, resp.AverageQualityScore)
	require.Empty(t, resp.TopTopics)
	require.Nil(t, resp.RisksCount)
	require.Nil(t, resp.RecommendationsCount)
	require.Len(t, resp.Charts.CallsByDay, 1)
	require.Empty(t, resp.Charts.AnalyzedByDay)
}

func TestGetOverviewRejectsInvalidScope(t *testing.T) {
	handler := NewHandler(fakeAnalyticsService{t: t})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/overview?scope=team", nil)
	req = req.WithContext(middleware.ContextWithUserID(req.Context(), uuid.New()))
	rec := httptest.NewRecorder()

	handler.GetOverview(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}
