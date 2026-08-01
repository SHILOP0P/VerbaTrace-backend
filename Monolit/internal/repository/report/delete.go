package report

import (
	"context"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `
	DELETE FROM call_report_exports
	WHERE report_uuid = $1
	`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete report export: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete report export: %w", err)
	}
	if rowsAffected == 0 {
		return models.ErrReportNotFound
	}

	return nil
}
