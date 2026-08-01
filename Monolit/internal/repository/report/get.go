package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) GetByUUID(ctx context.Context, id uuid.UUID) (models.ReportExport, error) {
	query := `
	SELECT ` + reportColumns + `
	FROM call_report_exports
	WHERE report_uuid = $1
	`

	report, err := scanReport(r.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ReportExport{}, models.ErrReportNotFound
		}
		return models.ReportExport{}, fmt.Errorf("get report export: %w", err)
	}

	return report, nil
}
