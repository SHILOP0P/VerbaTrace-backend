package analytics

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestAggregateReportGeneratorMarkdownAndXLSX(t *testing.T) {
	text := "fallback text"
	analysis := models.AggregateAnalysis{
		ID: uuid.New(), Scope: models.AggregateAnalysisScopePersonal,
		PeriodFrom: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodTo:   time.Date(2026, 7, 7, 23, 59, 59, 0, time.UTC),
		Status:     models.AggregateAnalysisStatusDone, SourceCallsCount: 3,
		ResultJSON: []byte(`{"summary":"summary text","priority_actions":[{"title":"Перезвонить клиенту","priority":"high","expected_effect":"Уточнить решение"}]}`),
		ResultText: &text,
	}
	md, err := generateAggregateReport(models.ReportFormatMD, AggregateReportData{Analysis: analysis, GeneratedAt: analysis.PeriodTo})
	require.NoError(t, err)
	require.Contains(t, string(md), "summary text")
	require.Contains(t, string(md), "Перезвонить клиенту")

	xlsx, err := generateAggregateReport(models.ReportFormatXLSX, AggregateReportData{Analysis: analysis, GeneratedAt: analysis.PeriodTo})
	require.NoError(t, err)
	require.NotEmpty(t, xlsx)
}

func TestAggregateReportExportsFullSourceDatasetInEveryFormat(t *testing.T) {
	analysis := models.AggregateAnalysis{
		ID: uuid.New(), Scope: models.AggregateAnalysisScopeCompany,
		PeriodFrom: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodTo:   time.Date(2026, 7, 7, 23, 59, 59, 0, time.UTC),
		Status:     models.AggregateAnalysisStatusDone, SourceCallsCount: 120,
		ResultJSON: []byte(`{
			"summary":"Краткая оценка по выборке звонков",
			"executive_summary":"Резюме для руководителя по всем звонкам периода",
			"detailed_report":{"methodology":"Анализ основан на данных готовых оценок звонков.","quality_overview":"Качество ответов требует внимания.","issue_analysis":"Проблемы повторяются в нескольких звонках.","customer_loss_analysis":"Риск потери клиентов связан с ценой.","training_plan":"Нужно усилить обучение менеджеров.","data_limitations":"Выводы основаны на доступных звонках."},
			"source_summary":{"analyzed_calls":120,"included_in_statistics":120,"representative_calls":40,"all_analyzed_calls_used":true,"source_set_hash":"full-source-set"},
			"coverage_note":"Статистика рассчитана по всем 120 готовым анализам звонков.",
			"aggregate_statistics":{
				"score_summary":{"calls_with_score":120,"average":76.5,"min":20,"max":98,"low_count":12,"medium_count":63,"high_count":45},
				"issue_coverage":[{"code":"no_next_step","title":"Нет следующего шага","count":42,"share":0.35,"sample_call_uuids":["11111111-1111-1111-1111-111111111111"]}],
				"weak_criteria":[{"code":"needs_discovery","title":"Выявление потребностей","applicable_calls":120,"weak_calls":30,"weak_share":0.25,"average_points_share":0.5,"missed_calls":10,"partially_met_calls":20,"unclear_calls":0,"sample_call_uuids":["22222222-2222-2222-2222-222222222222"]}],
				"business_outcomes":[{"code":"lost","title":"Потеря","count":18,"share":0.15}],
				"lost_reasons":[{"code":"price","title":"Цена","count":10,"share":0.0833}],
				"customer_objections":[{"code":"budget","title":"Бюджет","count":21,"share":0.175}],
				"risks":[{"code":"churn","title":"Риск ухода","count":8,"share":0.0667}],
				"topics":[{"code":"onboarding","title":"Подключение услуги","count":70,"share":0.5833}],
				"next_step_summary":{"calls_with_next_step":80,"calls_with_specific_next_step":60,"calls_missing_next_step":40,"calls_missing_specific_step":60,"missing_next_step_share":0.3333,"missing_specific_step_share":0.5},
				"attention_calls":[{"call_uuid":"33333333-3333-3333-3333-333333333333","created_at":"2026-07-02T10:00:00Z","title":"Звонок с риском","score":20,"summary":"Клиент может отказаться","issue_codes":["no_next_step"]}],
				"strong_calls":[{"call_uuid":"44444444-4444-4444-4444-444444444444","created_at":"2026-07-03T10:00:00Z","title":"Успешный звонок","score":98}]
			}
		}`),
	}
	data := AggregateReportData{Analysis: analysis, GeneratedAt: analysis.PeriodTo}

	markdown, err := generateAggregateReport(models.ReportFormatMD, data)
	require.NoError(t, err)
	require.Contains(t, string(markdown), "Статистика рассчитана по всем 120 готовым анализам звонков.")
	require.Contains(t, string(markdown), "Методика")
	require.Contains(t, string(markdown), "Покрытие проблем")
	require.Contains(t, string(markdown), "Выявление потребностей")
	require.Contains(t, string(markdown), "Звонок с риском от 02.07.2026")
	require.NotContains(t, string(markdown), "33333333-3333-3333-3333-333333333333")

	docx, err := generateAggregateReport(models.ReportFormatDOCX, data)
	require.NoError(t, err)
	require.Contains(t, docxDocument(t, docx), "Статистика построена по всем готовым анализам")
	require.Contains(t, docxDocument(t, docx), "Выявление потребностей")
	require.NotContains(t, docxDocument(t, docx), "33333333-3333-3333-3333-333333333333")
	require.Contains(t, docxDocument(t, docx), `w:color w:val="2B463C"`)

	xlsx, err := generateAggregateReport(models.ReportFormatXLSX, data)
	require.NoError(t, err)
	workbook, err := excelize.OpenReader(bytes.NewReader(xlsx))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, workbook.Close()) })
	require.Contains(t, workbook.GetSheetList(), "Покрытие проблем")
	require.Contains(t, workbook.GetSheetList(), "Слабые критерии")
	require.Contains(t, workbook.GetSheetList(), "Требуют внимания")
	issue, err := workbook.GetCellValue("Покрытие проблем", "A2")
	require.NoError(t, err)
	require.Equal(t, "Нет следующего шага", issue)
	styleID, err := workbook.GetCellStyle("Сводка", "A1")
	require.NoError(t, err)
	require.NotZero(t, styleID)
	require.NotContains(t, zipText(t, xlsx), "33333333-3333-3333-3333-333333333333")

	pdf, err := generateAggregateReport(models.ReportFormatPDF, data)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(pdf, []byte("%PDF")))
}

func docxDocument(t *testing.T, content []byte) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	require.NoError(t, err)
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		body, err := file.Open()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, body.Close()) })
		document, err := io.ReadAll(body)
		require.NoError(t, err)
		return string(document)
	}
	t.Fatal("word/document.xml is missing")
	return ""
}

func zipText(t *testing.T, content []byte) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	require.NoError(t, err)
	var text strings.Builder
	for _, file := range reader.File {
		body, err := file.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(body)
		require.NoError(t, body.Close())
		require.NoError(t, err)
		text.Write(data)
	}
	return text.String()
}

func TestAggregateReportGeneratorFallsBackOnMalformedJSON(t *testing.T) {
	text := "plain result"
	analysis := models.AggregateAnalysis{ID: uuid.New(), ResultJSON: []byte(`{bad`), ResultText: &text}
	content, err := generateAggregateReport(models.ReportFormatMD, AggregateReportData{Analysis: analysis, GeneratedAt: time.Now()})
	require.NoError(t, err)
	require.Contains(t, string(content), "plain result")
}

func TestCreateAggregateReportRequiresDoneAnalysis(t *testing.T) {
	userID := uuid.New()
	analysis := aggregateReportAnalysis(userID)
	analysis.Status = models.AggregateAnalysisStatusProcessing
	svc := NewService(&aggregateReportAnalyticsRepo{analysis: analysis})
	svc.SetReportRepository(&aggregateReportRepo{})
	svc.SetReportStorage(&aggregateReportStorage{})

	_, err := svc.CreateAggregateReport(context.Background(), models.CreateAggregateReportInput{
		AggregateAnalysisUUID: analysis.ID, UserUUID: userID, Format: models.ReportFormatMD,
	})
	require.ErrorIs(t, err, models.ErrInvalidAnalysisStatus)
}

func TestCreateDownloadDeleteAggregateReport(t *testing.T) {
	userID := uuid.New()
	analysis := aggregateReportAnalysis(userID)
	reports := &aggregateReportRepo{}
	storage := &aggregateReportStorage{files: map[string][]byte{}}
	svc := NewService(&aggregateReportAnalyticsRepo{analysis: analysis})
	svc.SetReportRepository(reports)
	svc.SetReportStorage(storage)

	report, err := svc.CreateAggregateReport(context.Background(), models.CreateAggregateReportInput{
		AggregateAnalysisUUID: analysis.ID, UserUUID: userID, Format: models.ReportFormatMD,
	})
	require.NoError(t, err)
	require.Equal(t, models.ReportStatusReady, report.Status)

	list, err := svc.ListAggregateReports(context.Background(), analysis.ID, userID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	file, err := svc.GetAggregateReportFile(context.Background(), report.ID, userID)
	require.NoError(t, err)
	body, _ := io.ReadAll(file.Content)
	require.Contains(t, string(body), "Резюме для руководителя")

	require.NoError(t, svc.DeleteAggregateReport(context.Background(), report.ID, userID))
	_, err = svc.GetAggregateReportFile(context.Background(), report.ID, userID)
	require.ErrorIs(t, err, models.ErrAggregateReportNotFound)
}

func TestAggregateReportMissingStorageFile(t *testing.T) {
	userID := uuid.New()
	analysis := aggregateReportAnalysis(userID)
	reportID := uuid.New()
	path := "missing.md"
	reports := &aggregateReportRepo{reports: map[uuid.UUID]models.AggregateReportExport{reportID: {
		ID: reportID, AggregateAnalysisUUID: analysis.ID, RequestedByUserUUID: userID,
		Format: models.ReportFormatMD, Status: models.ReportStatusReady, StoragePath: &path, ExpiresAt: time.Now().Add(time.Hour),
	}}}
	svc := NewService(&aggregateReportAnalyticsRepo{analysis: analysis})
	svc.SetReportRepository(reports)
	svc.SetReportStorage(&aggregateReportStorage{files: map[string][]byte{}})

	_, err := svc.GetAggregateReportFile(context.Background(), reportID, userID)
	require.ErrorIs(t, err, models.ErrAggregateReportFileNotFound)
}

func aggregateReportAnalysis(userID uuid.UUID) models.AggregateAnalysis {
	return models.AggregateAnalysis{
		ID: uuid.New(), Scope: models.AggregateAnalysisScopePersonal,
		UserUUID: uuid.NullUUID{UUID: userID, Valid: true}, CreatedByUserUUID: userID,
		PeriodFrom: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		PeriodTo:   time.Date(2026, 7, 7, 23, 59, 59, 0, time.UTC),
		Status:     models.AggregateAnalysisStatusDone, SourceCallsCount: 2,
		ResultJSON: []byte(`{"summary":"Краткое резюме","priority_actions":[{"title":"Действие","priority":"medium","expected_effect":"Улучшить качество"}]}`),
	}
}

type aggregateReportAnalyticsRepo struct {
	analyticsRepoStub
	analysis models.AggregateAnalysis
}

func (r *aggregateReportAnalyticsRepo) GetAggregateAnalysisByUUID(_ context.Context, id uuid.UUID) (models.AggregateAnalysis, error) {
	if id != r.analysis.ID {
		return models.AggregateAnalysis{}, models.ErrAggregateAnalysisNotFound
	}
	return r.analysis, nil
}

type aggregateReportRepo struct {
	reports map[uuid.UUID]models.AggregateReportExport
}

func (r *aggregateReportRepo) ensure() {
	if r.reports == nil {
		r.reports = map[uuid.UUID]models.AggregateReportExport{}
	}
}

func (r *aggregateReportRepo) CreateAggregate(_ context.Context, report models.AggregateReportExport) (models.AggregateReportExport, error) {
	r.ensure()
	r.reports[report.ID] = report
	return report, nil
}

func (r *aggregateReportRepo) MarkAggregateReady(_ context.Context, input models.MarkAggregateReportReadyInput) (models.AggregateReportExport, error) {
	r.ensure()
	report := r.reports[input.ID]
	report.Status = models.ReportStatusReady
	report.StoragePath = &input.StoragePath
	report.FileName = input.FileName
	report.ContentType = input.ContentType
	report.SizeBytes = input.SizeBytes
	r.reports[input.ID] = report
	return report, nil
}

func (r *aggregateReportRepo) MarkAggregateFailed(_ context.Context, input models.MarkAggregateReportFailedInput) (models.AggregateReportExport, error) {
	r.ensure()
	report := r.reports[input.ID]
	report.Status = models.ReportStatusFailed
	report.ErrorMessage = &input.ErrorMessage
	r.reports[input.ID] = report
	return report, nil
}

func (r *aggregateReportRepo) GetAggregateByUUID(_ context.Context, id uuid.UUID) (models.AggregateReportExport, error) {
	r.ensure()
	report, ok := r.reports[id]
	if !ok {
		return models.AggregateReportExport{}, models.ErrAggregateReportNotFound
	}
	return report, nil
}

func (r *aggregateReportRepo) ListAggregateByAnalysisUUID(_ context.Context, analysisID uuid.UUID, _ time.Time) ([]models.AggregateReportExport, error) {
	r.ensure()
	out := []models.AggregateReportExport{}
	for _, report := range r.reports {
		if report.AggregateAnalysisUUID == analysisID {
			out = append(out, report)
		}
	}
	return out, nil
}

func (r *aggregateReportRepo) DeleteAggregate(_ context.Context, id uuid.UUID) error {
	r.ensure()
	if _, ok := r.reports[id]; !ok {
		return models.ErrAggregateReportNotFound
	}
	delete(r.reports, id)
	return nil
}

type aggregateReportStorage struct {
	files map[string][]byte
}

func (s *aggregateReportStorage) Save(_ context.Context, input models.SaveReportInput) (models.SavedReportFile, error) {
	if input.Content == nil || input.AggregateAnalysisUUID == uuid.Nil {
		return models.SavedReportFile{}, models.ErrInvalidReportInput
	}
	if s.files == nil {
		s.files = map[string][]byte{}
	}
	body, _ := io.ReadAll(input.Content)
	path := strings.Join([]string{"aggregate", input.AggregateAnalysisUUID.String(), input.ReportUUID.String()}, "/")
	s.files[path] = body
	return models.SavedReportFile{Path: path, MimeType: input.MimeType, SizeBytes: int64(len(body))}, nil
}

func (s *aggregateReportStorage) Open(_ context.Context, path string) (io.ReadCloser, error) {
	body, ok := s.files[path]
	if !ok {
		return nil, models.ErrReportFileNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func (s *aggregateReportStorage) Delete(_ context.Context, path string) error {
	if s.files == nil {
		return nil
	}
	delete(s.files, path)
	return nil
}
