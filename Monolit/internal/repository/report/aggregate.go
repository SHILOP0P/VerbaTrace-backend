package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const aggregateReportColumns = `
	report_uuid,
	aggregate_analysis_uuid,
	requested_by_user_uuid,
	format,
	status,
	storage_path,
	file_name,
	content_type,
	size_bytes,
	error_message,
	created_at,
	updated_at,
	expires_at
`

func (r *Repository) CreateAggregate(ctx context.Context, report models.AggregateReportExport) (models.AggregateReportExport, error) {
	row := r.db.QueryRowContext(ctx, `INSERT INTO aggregate_report_exports (
		report_uuid, aggregate_analysis_uuid, requested_by_user_uuid, format, status, storage_path,
		file_name, content_type, size_bytes, error_message, created_at, updated_at, expires_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+aggregateReportColumns,
		report.ID, report.AggregateAnalysisUUID, report.RequestedByUserUUID, report.Format, report.Status, report.StoragePath,
		report.FileName, report.ContentType, report.SizeBytes, report.ErrorMessage, report.CreatedAt, report.UpdatedAt, report.ExpiresAt)
	return scanAggregateReport(row, "create aggregate report")
}

func (r *Repository) MarkAggregateReady(ctx context.Context, input models.MarkAggregateReportReadyInput) (models.AggregateReportExport, error) {
	row := r.db.QueryRowContext(ctx, `UPDATE aggregate_report_exports
		SET status = 'ready', storage_path = $2, file_name = $3, content_type = $4, size_bytes = $5, error_message = NULL, updated_at = now()
		WHERE report_uuid = $1 RETURNING `+aggregateReportColumns,
		input.ID, input.StoragePath, input.FileName, input.ContentType, input.SizeBytes)
	return scanAggregateReport(row, "mark aggregate report ready")
}

func (r *Repository) MarkAggregateFailed(ctx context.Context, input models.MarkAggregateReportFailedInput) (models.AggregateReportExport, error) {
	row := r.db.QueryRowContext(ctx, `UPDATE aggregate_report_exports
		SET status = 'failed', error_message = $2, updated_at = now()
		WHERE report_uuid = $1 RETURNING `+aggregateReportColumns, input.ID, input.ErrorMessage)
	return scanAggregateReport(row, "mark aggregate report failed")
}

func (r *Repository) GetAggregateByUUID(ctx context.Context, id uuid.UUID) (models.AggregateReportExport, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+aggregateReportColumns+` FROM aggregate_report_exports WHERE report_uuid = $1`, id)
	return scanAggregateReport(row, "get aggregate report")
}

func (r *Repository) ListAggregateByAnalysisUUID(ctx context.Context, analysisID uuid.UUID, now time.Time) ([]models.AggregateReportExport, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+aggregateReportColumns+`
		FROM aggregate_report_exports
		WHERE aggregate_analysis_uuid = $1 AND expires_at > $2
		ORDER BY created_at DESC`, analysisID, now)
	if err != nil {
		return nil, fmt.Errorf("list aggregate reports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	reports := []models.AggregateReportExport{}
	for rows.Next() {
		report, err := scanAggregateReport(rows, "scan aggregate report")
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate aggregate reports: %w", err)
	}
	return reports, nil
}

func (r *Repository) DeleteAggregate(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM aggregate_report_exports WHERE report_uuid = $1`, id)
	if err != nil {
		return fmt.Errorf("delete aggregate report: %w", err)
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return models.ErrAggregateReportNotFound
	}
	return nil
}

func scanAggregateReport(row interface{ Scan(dest ...any) error }, operation string) (models.AggregateReportExport, error) {
	var report models.AggregateReportExport
	err := row.Scan(
		&report.ID,
		&report.AggregateAnalysisUUID,
		&report.RequestedByUserUUID,
		&report.Format,
		&report.Status,
		&report.StoragePath,
		&report.FileName,
		&report.ContentType,
		&report.SizeBytes,
		&report.ErrorMessage,
		&report.CreatedAt,
		&report.UpdatedAt,
		&report.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.AggregateReportExport{}, models.ErrAggregateReportNotFound
		}
		return models.AggregateReportExport{}, fmt.Errorf("%s: %w", operation, err)
	}
	return report, nil
}
