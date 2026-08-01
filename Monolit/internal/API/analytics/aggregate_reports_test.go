package analytics

import (
	"context"
	"io"
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

func TestCreateAggregateReportResponseShape(t *testing.T) {
	userID := uuid.New()
	analysisID := uuid.New()
	reportID := uuid.New()
	handler := NewHandler(aggregateReportAPIService{report: aggregateAPIReport(reportID, analysisID, userID)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/deep-analyses/"+analysisID.String()+"/reports", strings.NewReader(`{"format":"md"}`))
	req = req.WithContext(middleware.ContextWithUserID(chiContext(req.Context(), "uuid", analysisID.String()), userID))
	rec := httptest.NewRecorder()

	handler.CreateAggregateReport(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Contains(t, rec.Body.String(), `"aggregate_analysis_uuid":"`+analysisID.String()+`"`)
	require.Contains(t, rec.Body.String(), `/api/v1/analytics/deep-analysis-reports/`+reportID.String()+`/download`)
}

func TestDownloadAggregateReportHeaders(t *testing.T) {
	userID := uuid.New()
	analysisID := uuid.New()
	reportID := uuid.New()
	report := aggregateAPIReport(reportID, analysisID, userID)
	report.FileName = "deep.md"
	report.ContentType = "text/markdown; charset=utf-8"
	report.SizeBytes = 7
	handler := NewHandler(aggregateReportAPIService{file: models.AggregateReportFile{Report: report, Content: io.NopCloser(strings.NewReader("content"))}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/deep-analysis-reports/"+reportID.String()+"/download", nil)
	req = req.WithContext(middleware.ContextWithUserID(chiContext(req.Context(), "report_uuid", reportID.String()), userID))
	rec := httptest.NewRecorder()

	handler.DownloadAggregateReport(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/markdown; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Equal(t, `attachment; filename="deep.md"`, rec.Header().Get("Content-Disposition"))
	require.Equal(t, "7", rec.Header().Get("Content-Length"))
	require.Equal(t, "content", rec.Body.String())
}

func TestDeleteAggregateReportNoContent(t *testing.T) {
	userID := uuid.New()
	reportID := uuid.New()
	handler := NewHandler(aggregateReportAPIService{})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/analytics/deep-analysis-reports/"+reportID.String(), nil)
	req = req.WithContext(middleware.ContextWithUserID(chiContext(req.Context(), "report_uuid", reportID.String()), userID))
	rec := httptest.NewRecorder()

	handler.DeleteAggregateReport(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestCreateAggregateReportRequiresAuth(t *testing.T) {
	handler := NewHandler(aggregateReportAPIService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/deep-analyses/"+uuid.NewString()+"/reports", strings.NewReader(`{"format":"md"}`))
	rec := httptest.NewRecorder()

	handler.CreateAggregateReport(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

type aggregateReportAPIService struct {
	fakeAnalyticsService
	report models.AggregateReportExport
	file   models.AggregateReportFile
}

func (s aggregateReportAPIService) CreateAggregateReport(context.Context, models.CreateAggregateReportInput) (models.AggregateReportExport, error) {
	return s.report, nil
}

func (s aggregateReportAPIService) ListAggregateReports(context.Context, uuid.UUID, uuid.UUID) ([]models.AggregateReportExport, error) {
	return []models.AggregateReportExport{s.report}, nil
}

func (s aggregateReportAPIService) GetAggregateReportFile(context.Context, uuid.UUID, uuid.UUID) (models.AggregateReportFile, error) {
	return s.file, nil
}

func (s aggregateReportAPIService) DeleteAggregateReport(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func aggregateAPIReport(reportID uuid.UUID, analysisID uuid.UUID, userID uuid.UUID) models.AggregateReportExport {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	return models.AggregateReportExport{
		ID: reportID, AggregateAnalysisUUID: analysisID, RequestedByUserUUID: userID,
		Format: models.ReportFormatMD, Status: models.ReportStatusReady,
		FileName: "deep.md", ContentType: "text/markdown; charset=utf-8", SizeBytes: 7,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
}

func chiContext(ctx context.Context, key string, value string) context.Context {
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add(key, value)
	return context.WithValue(ctx, chi.RouteCtxKey, routeContext)
}
