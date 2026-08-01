package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"
)

func (r *Repository) MarkReady(ctx context.Context, input models.MarkReportReadyInput) (models.ReportExport, error) {
	query := `
	UPDATE call_report_exports
	SET status = 'ready',
	    storage_path = $2,
	    file_name = $3,
	    content_type = $4,
	    size_bytes = $5,
	    error_message = NULL,
	    updated_at = now()
	WHERE report_uuid = $1
	RETURNING ` + reportColumns

	report, err := scanReport(r.db.QueryRowContext(ctx, query, input.ID, input.StoragePath, input.FileName, input.ContentType, input.SizeBytes))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ReportExport{}, models.ErrReportNotFound
		}
		return models.ReportExport{}, fmt.Errorf("mark report ready: %w", err)
	}

	return report, nil
}

func (r *Repository) MarkFailed(ctx context.Context, input models.MarkReportFailedInput) (models.ReportExport, error) {
	query := `
	UPDATE call_report_exports
	SET status = 'failed',
	    error_message = $2,
	    updated_at = now()
	WHERE report_uuid = $1
	RETURNING ` + reportColumns

	report, err := scanReport(r.db.QueryRowContext(ctx, query, input.ID, input.ErrorMessage))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ReportExport{}, models.ErrReportNotFound
		}
		return models.ReportExport{}, fmt.Errorf("mark report failed: %w", err)
	}

	return report, nil
}
