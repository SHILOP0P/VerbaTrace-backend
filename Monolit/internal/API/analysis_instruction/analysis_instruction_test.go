package analysis_instruction

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	serviceMocks "verbatrace/monolit/internal/service/mocks"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestHandlersSuccess(t *testing.T) {
	userID := uuid.New()
	instructionID := uuid.New()
	item := models.AnalysisInstruction{
		ID: instructionID, Scope: models.AnalysisInstructionScopePersonal,
		UserUUID: uuid.NullUUID{UUID: userID, Valid: true}, OriginalFilename: "guide.md",
		MimeType: "text/markdown", SizeBytes: 5, CreatedByUserUUID: userID,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	service := serviceMocks.NewAnalysisInstructionService(t)
	service.EXPECT().Create(mock.Anything, mock.Anything).Return(item, nil).Once()
	service.EXPECT().List(mock.Anything, mock.MatchedBy(func(input models.ListAnalysisInstructionsInput) bool {
		return input.IncludeInactive && input.Query == "guide" && input.Limit == 10 && input.Offset == 5
	})).Return([]models.AnalysisInstruction{item}, nil).Once()
	service.On("Get", mock.Anything, instructionID, userID).Return(item, nil).Once()
	service.On("Update", mock.Anything, mock.MatchedBy(func(input models.UpdateAnalysisInstructionInput) bool {
		return input.ID == instructionID && input.Title != nil && *input.Title == "New title"
	})).Return(item, nil).Once()
	service.On("ReplaceFile", mock.Anything, mock.MatchedBy(func(input models.ReplaceAnalysisInstructionFileInput) bool {
		return input.ID == instructionID && input.OriginalFilename == "guide.md"
	})).Return(item, nil).Once()
	service.EXPECT().GetFile(mock.Anything, mock.Anything, mock.Anything).Return(models.File{
		Content: io.NopCloser(strings.NewReader("guide")), OriginalFilename: "guide.md",
		MimeType: "text/markdown", SizeBytes: 5,
	}, nil).Once()
	service.On("Reorder", mock.Anything, mock.MatchedBy(func(input models.ReorderAnalysisInstructionsInput) bool {
		return input.Scope == models.AnalysisInstructionScopePersonal && len(input.Items) == 1 && input.Items[0].ID == instructionID
	})).Return(nil).Once()
	service.EXPECT().Delete(mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	handler := NewHandler(service)

	body, contentType := instructionMultipart(t, map[string]string{"scope": "personal"}, "guide.md", "guide")
	rec, req := instructionRequest(http.MethodPost, "/", body.String(), userID, nil)
	req.Header.Set("Content-Type", contentType)
	handler.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Create status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec, req = instructionRequest(http.MethodGet, "/?scope=personal&include_inactive=true&q=guide&limit=10&offset=5", "", userID, nil)
	handler.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("List status = %d", rec.Code)
	}

	rec, req = instructionRequest(http.MethodGet, "/", "", userID, map[string]string{"uuid": instructionID.String()})
	handler.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Get status=%d body=%q", rec.Code, rec.Body.String())
	}

	updateBody := `{"title":"New title","is_active":true,"sort_order":10}`
	rec, req = instructionRequest(http.MethodPatch, "/", updateBody, userID, map[string]string{"uuid": instructionID.String()})
	handler.Update(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Update status=%d body=%q", rec.Code, rec.Body.String())
	}

	body, contentType = instructionMultipart(t, nil, "guide.md", "updated")
	rec, req = instructionRequest(http.MethodPut, "/", body.String(), userID, map[string]string{"uuid": instructionID.String()})
	req.Header.Set("Content-Type", contentType)
	handler.ReplaceFile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ReplaceFile status=%d body=%q", rec.Code, rec.Body.String())
	}

	rec, req = instructionRequest(http.MethodGet, "/", "", userID, map[string]string{"uuid": instructionID.String()})
	handler.GetFile(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "guide" {
		t.Fatalf("GetFile status=%d body=%q", rec.Code, rec.Body.String())
	}
	if disposition := rec.Header().Get("Content-Disposition"); !strings.Contains(disposition, "guide.md") {
		t.Fatalf("Content-Disposition = %q", disposition)
	}

	reorderBody := `{"scope":"personal","items":[{"id":"` + instructionID.String() + `","sort_order":10}]}`
	rec, req = instructionRequest(http.MethodPatch, "/", reorderBody, userID, nil)
	handler.Reorder(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Reorder status=%d body=%q", rec.Code, rec.Body.String())
	}

	rec, req = instructionRequest(http.MethodDelete, "/", "", userID, map[string]string{"uuid": instructionID.String()})
	handler.Delete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("Delete status = %d", rec.Code)
	}
}

func TestValidationHelpersAndErrorMappings(t *testing.T) {
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	tests := []struct {
		scope, company, department string
		ok                         bool
	}{
		{"personal", "", "", true},
		{"company", companyID.String(), "", true},
		{"department", companyID.String(), departmentID.String(), true},
		{"personal", companyID.String(), "", false},
		{"company", "", "", false},
		{"department", companyID.String(), "", false},
		{"unknown", "", "", false},
	}
	for _, tt := range tests {
		_, _, _, _, err := parseInstructionPlacement(tt.scope, userID, tt.company, tt.department)
		if (err == nil) != tt.ok {
			t.Fatalf("placement %+v err=%v", tt, err)
		}
	}
	if _, err := parseInstructionUUID("bad"); err == nil {
		t.Fatal("expected invalid UUID error")
	}
	if got, err := parseInstructionUUID(companyID.String()); err != nil || got != companyID {
		t.Fatalf("parseInstructionUUID = %v, %v", got, err)
	}

	errorCases := []struct {
		err  error
		code int
	}{
		{models.ErrInvalidAnalysisInstructionInput, http.StatusBadRequest},
		{models.ErrUnsupportedInstructionType, http.StatusBadRequest},
		{models.ErrInstructionLimitExceeded, http.StatusBadRequest},
		{models.ErrSubscriptionRequired, http.StatusPaymentRequired},
		{models.ErrAnalysisInstructionNotFound, http.StatusNotFound},
		{models.ErrCompanyNotFound, http.StatusNotFound},
		{models.ErrDepartmentNotFound, http.StatusNotFound},
		{models.ErrForbidden, http.StatusForbidden},
		{errors.New("db"), http.StatusInternalServerError},
	}
	for _, tt := range errorCases {
		rec := httptest.NewRecorder()
		writeInstructionError(rec, tt.err, "fallback", "failed")
		if rec.Code != tt.code {
			t.Fatalf("instruction error %v: %d", tt.err, rec.Code)
		}
	}

	fileErrors := []struct {
		err  error
		code int
	}{
		{models.ErrInvalidAnalysisInstructionInput, http.StatusBadRequest},
		{models.ErrAnalysisInstructionNotFound, http.StatusNotFound},
		{models.ErrInstructionFileNotFound, http.StatusNotFound},
		{models.ErrCompanyNotFound, http.StatusNotFound},
		{models.ErrDepartmentNotFound, http.StatusNotFound},
		{models.ErrForbidden, http.StatusForbidden},
		{errors.New("db"), http.StatusInternalServerError},
	}
	for _, tt := range fileErrors {
		rec := httptest.NewRecorder()
		writeInstructionFileError(rec, tt.err)
		if rec.Code != tt.code {
			t.Fatalf("file error %v: %d", tt.err, rec.Code)
		}
	}
}

func TestHandlersRejectInvalidRequests(t *testing.T) {
	handler := NewHandler(serviceMocks.NewAnalysisInstructionService(t))
	for _, method := range []func(http.ResponseWriter, *http.Request){handler.Create, handler.List, handler.Get, handler.Update, handler.ReplaceFile, handler.GetFile, handler.Reorder, handler.Delete} {
		rec, req := instructionRequest(http.MethodGet, "/", "", uuid.Nil, nil)
		method(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized status = %d", rec.Code)
		}
	}

	rec, req := instructionRequest(http.MethodPost, "/", "invalid", uuid.New(), nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	handler.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid multipart status = %d", rec.Code)
	}

	rec, req = instructionRequest(http.MethodGet, "/?scope=unknown", "", uuid.New(), nil)
	handler.List(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid list status = %d", rec.Code)
	}

	for _, method := range []func(http.ResponseWriter, *http.Request){handler.Get, handler.Update, handler.ReplaceFile, handler.GetFile, handler.Delete} {
		rec, req = instructionRequest(http.MethodGet, "/", "", uuid.New(), map[string]string{"uuid": "bad"})
		method(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid UUID status = %d", rec.Code)
		}
	}

	rec, req = instructionRequest(http.MethodPatch, "/", `{"scope":"personal"}`, uuid.New(), map[string]string{"uuid": uuid.New().String()})
	handler.Update(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown update field status = %d", rec.Code)
	}

	rec, req = instructionRequest(http.MethodPatch, "/", `{"scope":"personal","items":[{"id":"bad","sort_order":1}]}`, uuid.New(), nil)
	handler.Reorder(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad reorder uuid status = %d", rec.Code)
	}
}

func instructionRequest(method, path, body string, userID uuid.UUID, params map[string]string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
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

func instructionMultipart(t *testing.T, fields map[string]string, name, content string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(content))
	_ = writer.Close()
	return body, writer.FormDataContentType()
}

func TestAnalysisInstructionResponseDoesNotContainFilePath(t *testing.T) {
	item := models.AnalysisInstruction{ID: uuid.New(), FilePath: "local/path.md"}
	resp, err := json.Marshal(mustInstructionResponse(t, item))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(resp, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["file_path"]; ok {
		t.Fatalf("response leaked file_path: %s", resp)
	}
	if fields["download_url"] == "" {
		t.Fatalf("missing download_url: %s", resp)
	}
}

func mustInstructionResponse(t *testing.T, item models.AnalysisInstruction) any {
	t.Helper()
	resp, err := converter.AnalysisInstructionModelToAPI(item)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
