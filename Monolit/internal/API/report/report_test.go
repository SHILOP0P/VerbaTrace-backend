package report

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	serviceMocks "verbatrace/monolit/internal/service/mocks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestReportHandlersSuccess(t *testing.T) {
	userID := uuid.New()
	callID := uuid.New()
	reportID := uuid.New()
	path := "report.md"
	item := models.ReportExport{
		ID: reportID, CallUUID: callID, Format: models.ReportFormatMD,
		Status: models.ReportStatusReady, StoragePath: &path, FileName: "report.md",
		ContentType: "text/markdown", SizeBytes: 4, CreatedAt: time.Now(), UpdatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}
	service := serviceMocks.NewReportService(t)
	service.EXPECT().Create(mock.Anything, mock.Anything).Return(item, nil).Once()
	service.EXPECT().CreateGlobal(mock.Anything, mock.MatchedBy(func(input models.CreateGlobalReportInput) bool {
		return input.UserUUID == userID && input.CallUUID.Valid && input.CallUUID.UUID == callID && input.Scope == models.ReportScopeCall
	})).Return(item, nil).Once()
	service.EXPECT().List(mock.Anything, mock.MatchedBy(func(input models.ListReportsInput) bool {
		return input.UserUUID == userID &&
			input.Format == models.ReportFormatMD &&
			input.Status == models.ReportStatusReady &&
			input.CallUUID.Valid &&
			input.CallUUID.UUID == callID &&
			input.Sort == models.ReportSortCreatedAt &&
			input.Order == models.SortOrderDesc &&
			input.Limit == 1
	})).Return(models.ListReportsResult{
		Reports: []models.ReportWithCall{{
			Report: item,
			Call: models.ReportCallSummary{
				ID:             callID,
				Title:          "Обсуждение условий договора",
				Status:         models.CallStatusAnalyzed,
				CreatedAt:      item.CreatedAt,
				CompanyUUID:    uuid.NullUUID{UUID: uuid.New(), Valid: true},
				DepartmentUUID: uuid.NullUUID{},
			},
		}},
		Total:  1,
		Limit:  1,
		Offset: 0,
	}, nil).Once()
	service.EXPECT().ListByCallUUID(mock.Anything, mock.Anything, mock.Anything).
		Return([]models.ReportExport{item}, nil).Once()
	service.EXPECT().GetFile(mock.Anything, mock.Anything, mock.Anything).
		Return(models.ReportFile{Report: item, Content: io.NopCloser(strings.NewReader("data"))}, nil).Once()
	service.EXPECT().Delete(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	handler := NewHandler(service)

	rec, req := reportRequest(http.MethodPost, `{"format":"md"}`, userID, map[string]string{"uuid": callID.String()})
	handler.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Create status = %d", rec.Code)
	}

	rec, req = reportRequest(http.MethodPost, `{"format":"md","scope":"call","call_uuid":"`+callID.String()+`"}`, userID, nil)
	handler.CreateGlobal(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("CreateGlobal status = %d", rec.Code)
	}

	rec, req = reportRequest(http.MethodGet, "", userID, nil)
	req.URL.RawQuery = "format=md&status=ready&call_uuid=" + callID.String() + "&limit=1&sort=created_at&order=desc"
	handler.List(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"total":1`) || !strings.Contains(rec.Body.String(), `"call":`) {
		t.Fatalf("Global List status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec, req = reportRequest(http.MethodGet, "", userID, map[string]string{"uuid": callID.String()})
	handler.ListByCallUUID(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("List status = %d", rec.Code)
	}

	rec, req = reportRequest(http.MethodGet, "", userID, map[string]string{"report_uuid": reportID.String()})
	handler.Download(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "data" {
		t.Fatalf("Download status=%d body=%q", rec.Code, rec.Body.String())
	}

	rec, req = reportRequest(http.MethodDelete, "", userID, map[string]string{"report_uuid": reportID.String()})
	handler.Delete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Delete status = %d", rec.Code)
	}

	if got := contentDisposition("отчет 1.md"); !strings.Contains(got, "attachment;") {
		t.Fatalf("contentDisposition = %q", got)
	}
}

func TestReportHandlersValidationAndErrors(t *testing.T) {
	service := serviceMocks.NewReportService(t)
	handler := NewHandler(service)
	for _, method := range []func(http.ResponseWriter, *http.Request){handler.Create, handler.CreateGlobal, handler.List, handler.ListByCallUUID, handler.Download, handler.Delete} {
		rec, req := reportRequest(http.MethodGet, "", uuid.Nil, nil)
		method(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized status = %d", rec.Code)
		}
	}

	userID := uuid.New()
	rec, req := reportRequest(http.MethodPost, `{`, userID, map[string]string{"uuid": uuid.NewString()})
	handler.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid body status = %d", rec.Code)
	}
	rec, req = reportRequest(http.MethodPost, `{`, userID, nil)
	handler.CreateGlobal(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid global body status = %d", rec.Code)
	}

	for _, rawQuery := range []string{
		"format=csv",
		"status=done",
		"company_uuid=bad",
		"from=bad",
		"sort=size",
		"order=sideways",
		"limit=0",
		"offset=-1",
	} {
		service.EXPECT().List(mock.Anything, mock.Anything).Return(models.ListReportsResult{}, models.ErrInvalidReportInput).Maybe()
		rec, req = reportRequest(http.MethodGet, "", userID, nil)
		req.URL.RawQuery = rawQuery
		handler.List(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad global query %q status = %d", rawQuery, rec.Code)
		}
	}

	for _, method := range []func(http.ResponseWriter, *http.Request){handler.Create, handler.ListByCallUUID} {
		rec, req = reportRequest(http.MethodGet, `{}`, userID, map[string]string{"uuid": "bad"})
		method(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad call UUID status = %d", rec.Code)
		}
	}
	for _, method := range []func(http.ResponseWriter, *http.Request){handler.Download, handler.Delete} {
		rec, req = reportRequest(http.MethodGet, "", userID, map[string]string{"report_uuid": "bad"})
		method(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("bad report UUID status = %d", rec.Code)
		}
	}

	errorsToMap := []struct {
		err  error
		code int
	}{
		{models.ErrCallNotFound, http.StatusNotFound},
		{models.ErrAnalysisNotFound, http.StatusNotFound},
		{models.ErrReportNotFound, http.StatusNotFound},
		{models.ErrUnsupportedReportFormat, http.StatusBadRequest},
		{models.ErrUnsupportedReportScope, http.StatusBadRequest},
		{models.ErrInvalidReportInput, http.StatusBadRequest},
		{models.ErrReportScopeNotImplemented, http.StatusNotImplemented},
		{models.ErrInvalidAnalysisStatus, http.StatusConflict},
		{models.ErrSubscriptionRequired, http.StatusPaymentRequired},
		{models.ErrExportAccessDenied, http.StatusForbidden},
		{models.ErrForbidden, http.StatusForbidden},
		{models.ErrReportNotReady, http.StatusConflict},
		{models.ErrReportExpired, http.StatusGone},
		{models.ErrReportFileNotFound, http.StatusGone},
		{errors.New("db"), http.StatusInternalServerError},
	}
	for _, tt := range errorsToMap {
		rec := httptest.NewRecorder()
		writeReportError(rec, tt.err, "fallback")
		if rec.Code != tt.code {
			t.Fatalf("error %v: status=%d want=%d", tt.err, rec.Code, tt.code)
		}
	}
}

func reportRequest(method, body string, userID uuid.UUID, params map[string]string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	if userID != uuid.Nil {
		req = req.WithContext(middleware.ContextWithUserID(req.Context(), userID))
	}
	route := chi.NewRouteContext()
	for key, value := range params {
		route.URLParams.Add(key, value)
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
	return httptest.NewRecorder(), req
}
