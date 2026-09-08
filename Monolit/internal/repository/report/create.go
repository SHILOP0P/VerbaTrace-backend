package report

import (
	"context"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func (r *Repository) Create(ctx context.Context, report models.ReportExport) (models.ReportExport, error) {
	if report.Content == "" {
		report.Content = "full"
	}
	var analysisID any
	if report.AnalysisUUID != uuid.Nil {
		analysisID = report.AnalysisUUID
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.ReportExport{}, fmt.Errorf("begin create report export: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", report.CallUUID.String()+":"+string(report.Format)); err != nil {
		return models.ReportExport{}, fmt.Errorf("lock create report export: %w", err)
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM call_report_exports WHERE call_uuid=$1 AND format=$2 AND content=$3 AND transcription_revision=$4 AND analysis_uuid IS NOT DISTINCT FROM $5::uuid AND status IN ('pending','ready') AND expires_at>now())", report.CallUUID, report.Format, report.Content, report.TranscriptionRevision, analysisID).Scan(&exists); err != nil {
		return models.ReportExport{}, fmt.Errorf("check duplicate report export: %w", err)
	}
	if exists {
		return models.ReportExport{}, models.ErrReportAlreadyExists
	}
	query := `
	INSERT INTO call_report_exports (
		report_uuid,
		call_uuid,
		analysis_uuid,
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
		expires_at, content, transcription_revision
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
	)
	RETURNING ` + reportColumns

	row := tx.QueryRowContext(
		ctx,
		query,
		report.ID,
		report.CallUUID,
		analysisID,
		report.RequestedByUserUUID,
		report.Format,
		report.Status,
		report.StoragePath,
		report.FileName,
		report.ContentType,
		report.SizeBytes,
		report.ErrorMessage,
		report.CreatedAt,
		report.UpdatedAt,
		report.ExpiresAt, report.Content, report.TranscriptionRevision,
	)

	created, err := scanReport(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return models.ReportExport{}, models.ErrReportAlreadyExists
		}
		return models.ReportExport{}, fmt.Errorf("create report export: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return models.ReportExport{}, fmt.Errorf("commit create report export: %w", err)
	}

	return created, nil
}
