package report

import (
	"context"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const defaultListReportsLimit = 20

func (r *Repository) List(ctx context.Context, input models.ListReportsInput, now time.Time) (models.ListReportsResult, error) {
	if input.Limit <= 0 {
		input.Limit = defaultListReportsLimit
	}

	where, args := buildListReportFilters(input, now)
	limitParam := len(args) + 1
	args = append(args, input.Limit)
	offsetParam := len(args) + 1
	args = append(args, input.Offset)

	query := fmt.Sprintf(`
	SELECT `+prefixedReportColumns("r")+`,
	       c.call_uuid,
	       c.title,
	       c.status,
	       c.created_at,
	       c.company_uuid,
	       c.department_uuid,
	       COUNT(*) OVER() AS total
	FROM call_report_exports r
	JOIN calls c ON c.call_uuid = r.call_uuid
	WHERE %s
	ORDER BY %s %s
	LIMIT $%d OFFSET $%d
	`, where, reportSortColumn(input.Sort), reportSortOrder(input.Order), limitParam, offsetParam)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return models.ListReportsResult{}, fmt.Errorf("list report exports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	reports := make([]models.ReportWithCall, 0)
	total := 0
	for rows.Next() {
		item, rowTotal, err := scanReportWithCall(rows)
		if err != nil {
			return models.ListReportsResult{}, fmt.Errorf("list report exports: %w", err)
		}
		total = rowTotal
		reports = append(reports, item)
	}
	if err := rows.Err(); err != nil {
		return models.ListReportsResult{}, fmt.Errorf("list report exports: %w", err)
	}
	if len(reports) == 0 && input.Offset > 0 {
		total, err = r.countReports(ctx, input, now)
		if err != nil {
			return models.ListReportsResult{}, err
		}
	}

	return models.ListReportsResult{
		Reports: reports,
		Total:   total,
		Limit:   input.Limit,
		Offset:  input.Offset,
	}, nil
}

func (r *Repository) ListByCallUUID(ctx context.Context, callID uuid.UUID, now time.Time) ([]models.ReportExport, error) {
	query := `
	SELECT ` + reportColumns + `
	FROM call_report_exports
	WHERE call_uuid = $1
	  AND expires_at > $2
	ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, callID, now)
	if err != nil {
		return nil, fmt.Errorf("list report exports by call uuid: %w", err)
	}
	defer func() { _ = rows.Close() }()

	reports := make([]models.ReportExport, 0)
	for rows.Next() {
		report, err := scanReport(rows)
		if err != nil {
			return nil, fmt.Errorf("list report exports by call uuid: %w", err)
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list report exports by call uuid: %w", err)
	}

	return reports, nil
}

func (r *Repository) countReports(ctx context.Context, input models.ListReportsInput, now time.Time) (int, error) {
	where, args := buildListReportFilters(input, now)
	query := fmt.Sprintf(`
	SELECT COUNT(*)
	FROM call_report_exports r
	JOIN calls c ON c.call_uuid = r.call_uuid
	WHERE %s
	`, where)

	var total int
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count report exports: %w", err)
	}
	return total, nil
}

func buildListReportFilters(input models.ListReportsInput, now time.Time) (string, []any) {
	args := []any{input.UserUUID, now}
	conditions := []string{visibleToUserCondition("c", "$1"), "r.expires_at > $2"}

	if input.Format != "" {
		args = append(args, string(input.Format))
		conditions = append(conditions, fmt.Sprintf("r.format = $%d", len(args)))
	}
	if input.Status != "" {
		args = append(args, string(input.Status))
		conditions = append(conditions, fmt.Sprintf("r.status = $%d", len(args)))
	}
	if input.CompanyUUID.Valid {
		args = append(args, input.CompanyUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("c.company_uuid = $%d", len(args)))
	}
	if input.DepartmentUUID.Valid {
		args = append(args, input.DepartmentUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("c.department_uuid = $%d", len(args)))
	}
	if input.CallUUID.Valid {
		args = append(args, input.CallUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("r.call_uuid = $%d", len(args)))
	}
	if input.From != nil {
		args = append(args, *input.From)
		conditions = append(conditions, fmt.Sprintf("r.created_at >= $%d", len(args)))
	}
	if input.To != nil {
		args = append(args, *input.To)
		conditions = append(conditions, fmt.Sprintf("r.created_at <= $%d", len(args)))
	}

	return strings.Join(conditions, " AND "), args
}

func prefixedReportColumns(alias string) string {
	columns := []string{
		"report_uuid",
		"call_uuid",
		"analysis_uuid",
		"requested_by_user_uuid",
		"format",
		"status",
		"storage_path",
		"file_name",
		"content_type",
		"size_bytes",
		"error_message",
		"created_at",
		"updated_at",
		"expires_at",
		"content",
		"transcription_revision",
	}

	for i, column := range columns {
		columns[i] = alias + "." + column
	}
	return strings.Join(columns, ",\n\t       ")
}

func reportSortColumn(sort models.ReportSortField) string {
	switch sort {
	case models.ReportSortUpdatedAt:
		return "r.updated_at"
	default:
		return "r.created_at"
	}
}

func reportSortOrder(order models.SortOrder) string {
	if order == models.SortOrderAsc {
		return "ASC"
	}
	return "DESC"
}

func visibleToUserCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`
	(
	    %s.uploaded_by_user_uuid = %s
	    OR (
	        %s.company_uuid IS NOT NULL
	        AND EXISTS (
	            SELECT 1
	            FROM company_members cm
	            WHERE cm.company_uuid = %s.company_uuid
	              AND cm.user_uuid = %s
	              AND cm.role IN ('company_manager','company_deputy')
	              AND cm.status = 'active'
	        )
	    )
	    OR (
	        %s.department_uuid IS NOT NULL
	        AND EXISTS (
	            SELECT 1
	            FROM department_members dm
	            WHERE dm.department_uuid = %s.department_uuid
	              AND dm.user_uuid = %s
	              AND dm.role = 'department_leader'
	              AND dm.status = 'active'
	        )
	    )
	)`, callAlias, userParam, callAlias, callAlias, userParam, callAlias, callAlias, userParam)
}

func (r *Repository) ListExpiredReady(ctx context.Context, now time.Time, limit int) ([]models.ReportExport, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
	SELECT ` + reportColumns + `
	FROM call_report_exports
	WHERE status = 'ready'
	  AND expires_at <= $1
	ORDER BY expires_at ASC
	LIMIT $2
	`

	rows, err := r.db.QueryContext(ctx, query, now, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired report exports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	reports := make([]models.ReportExport, 0)
	for rows.Next() {
		report, err := scanReport(rows)
		if err != nil {
			return nil, fmt.Errorf("list expired report exports: %w", err)
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list expired report exports: %w", err)
	}

	return reports, nil
}
