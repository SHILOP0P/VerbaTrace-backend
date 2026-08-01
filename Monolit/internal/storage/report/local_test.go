package report

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func TestLocalStorageLifecycleAndFormats(t *testing.T) {
	storage := NewLocalStorage(t.TempDir())

	formats := map[models.ReportFormat]string{
		models.ReportFormatPDF:  ".pdf",
		models.ReportFormatDOCX: ".docx",
		models.ReportFormatMD:   ".md",
		models.ReportFormatXLSX: ".xlsx",
	}
	for format, ext := range formats {
		t.Run(string(format), func(t *testing.T) {
			callID := uuid.New()
			reportID := uuid.New()
			saved, err := storage.Save(context.Background(), models.SaveReportInput{
				ReportUUID: reportID, CallUUID: callID, Format: format,
				MimeType: "application/octet-stream", Content: strings.NewReader("report"),
			})
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			if filepath.Ext(saved.Path) != ext || saved.SizeBytes != 6 {
				t.Fatalf("saved file = %+v", saved)
			}

			file, err := storage.Open(context.Background(), saved.Path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			data, _ := io.ReadAll(file)
			_ = file.Close()
			if string(data) != "report" {
				t.Fatalf("content = %q", data)
			}
			if err := storage.Delete(context.Background(), saved.Path); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if _, err := storage.Open(context.Background(), saved.Path); !errors.Is(err, models.ErrReportFileNotFound) {
				t.Fatalf("Open missing error = %v", err)
			}
		})
	}
}

func TestLocalStorageValidationAndCancellation(t *testing.T) {
	storage := NewLocalStorage(t.TempDir())
	invalid := []models.SaveReportInput{
		{},
		{ReportUUID: uuid.New(), CallUUID: uuid.New(), Format: "csv", Content: strings.NewReader("x")},
		{ReportUUID: uuid.New(), CallUUID: uuid.New(), Format: models.ReportFormatMD, Content: strings.NewReader("")},
	}
	for i, input := range invalid {
		if _, err := storage.Save(context.Background(), input); err == nil {
			t.Fatalf("invalid input %d unexpectedly succeeded", i)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	valid := models.SaveReportInput{
		ReportUUID: uuid.New(), CallUUID: uuid.New(), Format: models.ReportFormatMD, Content: strings.NewReader("x"),
	}
	if _, err := storage.Save(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save canceled error = %v", err)
	}
	if _, err := storage.Open(ctx, "x.md"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open canceled error = %v", err)
	}
	if err := storage.Delete(ctx, "x.md"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete canceled error = %v", err)
	}
	if _, err := safeLocalPath(storage.baseDir, filepath.Join("..", "escape.md")); !errors.Is(err, models.ErrInvalidReportPath) {
		t.Fatalf("unsafe path error = %v", err)
	}
}
