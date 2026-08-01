package report

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (l *LocalStorage) Save(ctx context.Context, input models.SaveReportInput) (models.SavedReportFile, error) {
	if input.Content == nil || input.ReportUUID == uuid.Nil || (input.CallUUID == uuid.Nil && input.AggregateAnalysisUUID == uuid.Nil) {
		return models.SavedReportFile{}, models.ErrInvalidReportInput
	}

	ext, err := reportExtension(input.Format)
	if err != nil {
		return models.SavedReportFile{}, err
	}

	relativeDir := filepath.Join(input.CallUUID.String())
	if input.CallUUID == uuid.Nil {
		relativeDir = filepath.Join("aggregate", input.AggregateAnalysisUUID.String())
	}
	if err := os.MkdirAll(filepath.Join(l.baseDir, relativeDir), 0755); err != nil {
		return models.SavedReportFile{}, fmt.Errorf("creating report directory failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return models.SavedReportFile{}, ctx.Err()
	default:
	}

	relativePath := filepath.Join(relativeDir, input.ReportUUID.String()+ext)
	fullPath := filepath.Join(l.baseDir, relativePath)

	dst, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return models.SavedReportFile{}, fmt.Errorf("create report file failed: %w", err)
	}
	defer func() { _ = dst.Close() }()

	sizeBytes, err := io.Copy(dst, input.Content)
	if err != nil {
		_ = os.Remove(fullPath)
		return models.SavedReportFile{}, fmt.Errorf("save report file failed: %w", err)
	}
	if sizeBytes == 0 {
		_ = os.Remove(fullPath)
		return models.SavedReportFile{}, models.ErrInvalidReportInput
	}

	return models.SavedReportFile{
		Path:      relativePath,
		MimeType:  input.MimeType,
		SizeBytes: sizeBytes,
	}, nil
}

func (l *LocalStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	fullPath, err := safeLocalPath(l.baseDir, path)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %w", models.ErrReportFileNotFound, err)
		}

		return nil, fmt.Errorf("open report file failed: %w", err)
	}

	return file, nil
}

func (l *LocalStorage) Delete(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fullPath, err := safeLocalPath(l.baseDir, path)
	if err != nil {
		return err
	}

	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete report file failed: %w", err)
	}
	return nil
}

func reportExtension(format models.ReportFormat) (string, error) {
	switch format {
	case models.ReportFormatPDF:
		return ".pdf", nil
	case models.ReportFormatDOCX:
		return ".docx", nil
	case models.ReportFormatMD:
		return ".md", nil
	case models.ReportFormatXLSX:
		return ".xlsx", nil
	default:
		return "", models.ErrUnsupportedReportFormat
	}
}

func safeLocalPath(baseDir string, path string) (string, error) {
	cleanPath := filepath.Clean(path)

	if cleanPath == "." || cleanPath == ".." || filepath.IsAbs(cleanPath) {
		return "", models.ErrInvalidReportPath
	}

	if strings.HasPrefix(cleanPath, ".."+string(os.PathSeparator)) {
		return "", models.ErrInvalidReportPath
	}

	return filepath.Join(baseDir, cleanPath), nil
}
