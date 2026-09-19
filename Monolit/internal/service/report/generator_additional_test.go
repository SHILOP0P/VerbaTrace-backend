package report

import (
	"archive/zip"
	"bytes"
	"errors"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

func TestGenerateAllReportFormats(t *testing.T) {
	model := "test-model"
	text := "fallback text"
	data := ReportData{
		Call: models.Call{
			ID: uuid.New(), Title: "Test & Report", Status: models.CallStatusAnalyzed,
			DurationSeconds: 120, CreatedAt: time.Now().UTC(),
		},
		Analysis: models.CallAnalysis{
			ID: uuid.New(), Status: models.CallAnalysisStatusDone, Provider: "test", Model: &model,
			ResultText: &text,
			ResultJSON: []byte(`{
				"summary":"Summary","topics":["one"],"dialogue_tone":{"overall":"good"},
				"client_questions":[{"question":"Q","manager_answer":"A","answer_status":"answered"}],
				"question_coverage":{"status":"partially_answered"},
				"criteria_results":[{"result":"done"}],"confidence":"high","score":90,
				"next_steps":["next"],"manager_quality":{"strengths":["polite"]}
			}`),
		},
		TranscriptionText: "A: hello\nB: world",
		GeneratedAt:       time.Now().UTC(),
	}

	md, err := generateReport(models.ReportFormatMD, data)
	if err != nil || !bytes.Contains(md, []byte("# Отчет")) {
		t.Fatalf("markdown: size=%d err=%v", len(md), err)
	}
	docx, err := generateReport(models.ReportFormatDOCX, data)
	if err != nil || len(docx) == 0 {
		t.Fatalf("docx: size=%d err=%v", len(docx), err)
	}
	reader, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil || len(reader.File) != 3 {
		t.Fatalf("docx zip: files=%d err=%v", len(reader.File), err)
	}
	xlsx, err := generateReport(models.ReportFormatXLSX, data)
	if err != nil || len(xlsx) == 0 {
		t.Fatalf("xlsx: size=%d err=%v", len(xlsx), err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(xlsx))
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	_ = file.Close()

	pdf, err := generateReport(models.ReportFormatPDF, data)
	if err != nil || len(pdf) == 0 {
		t.Fatalf("pdf: size=%d err=%v", len(pdf), err)
	}

	if _, err := generateReport("csv", data); !errors.Is(err, models.ErrUnsupportedReportFormat) {
		t.Fatalf("unsupported format error = %v", err)
	}
}

func TestReportFormattingHelpers(t *testing.T) {
	formats := []models.ReportFormat{
		models.ReportFormatPDF, models.ReportFormatDOCX, models.ReportFormatMD, models.ReportFormatXLSX,
	}
	for _, format := range formats {
		if got, err := normalizeFormat(format); err != nil || got != format {
			t.Fatalf("normalizeFormat(%q) = %q, %v", format, got, err)
		}
		if contentType(format) == "application/octet-stream" || fileExtension(format) == "" {
			t.Fatalf("missing metadata for %q", format)
		}
	}
	if _, err := normalizeFormat("csv"); err == nil {
		t.Fatal("expected unsupported format error")
	}
	if contentType("csv") != "application/octet-stream" || fileExtension("csv") != "" {
		t.Fatal("unexpected fallback format metadata")
	}

	if got := xmlEscape(`<tag attr="x">&`); got != "&lt;tag attr=&#34;x&#34;&gt;&amp;" {
		t.Fatalf("xmlEscape = %q", got)
	}
	if got := splitParagraphs("one\r\ntwo"); len(got) != 2 {
		t.Fatalf("splitParagraphs = %#v", got)
	}
}

func TestAnalysisHelpers(t *testing.T) {
	for input, want := range map[string]string{
		"answered": "Ответ дан", "partially_answered": "Ответ частичный",
		"not_answered": "Ответ не дан", "no_questions": "Вопросов не было",
		"unclear": "Неясно", "custom": "custom", "": "Не указано",
	} {
		if got := answerStatusLabel(input); got != want {
			t.Fatalf("answerStatusLabel(%q) = %q", input, got)
		}
	}
	for input, want := range map[string]string{
		"high": "Высокая", "medium": "Средняя", "low": "Низкая", "": "Не указано",
	} {
		if got := confidenceLabel(input); got != want {
			t.Fatalf("confidenceLabel(%q) = %q", input, got)
		}
	}
	if scoreLabel(0) != "Не указана" || scoreLabel(87.6) != "88/100" {
		t.Fatal("score label mismatch")
	}

	data := ReportData{Analysis: models.CallAnalysis{ResultJSON: []byte(`{`)}}
	if data.StructuredAnalysis().Summary != "{" {
		t.Fatalf("invalid JSON fallback = %+v", data.StructuredAnalysis())
	}
	data = ReportData{}
	if data.StructuredAnalysis().Summary != "Не указано" {
		t.Fatalf("empty fallback = %+v", data.StructuredAnalysis())
	}
	if got := data.AnalysisJSONText(); got != "" {
		t.Fatalf("empty JSON text = %q", got)
	}
	raw := "not-json"
	data.Analysis.ResultJSON = []byte(raw)
	if got := data.AnalysisJSONText(); got != raw {
		t.Fatalf("raw JSON text = %q", got)
	}
}
