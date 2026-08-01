package analysis_instruction

import (
	"context"
	"database/sql"
	"fmt"

	model "verbatrace/monolit/internal/models"
)

func (r *Repository) Reorder(ctx context.Context, items []model.ReorderAnalysisInstructionItem) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin reorder analysis instructions: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, item := range items {
		result, err := tx.ExecContext(ctx, `
			UPDATE analysis_instructions
			SET sort_order = $2,
			    updated_at = now()
			WHERE instruction_uuid = $1
		`, item.ID, item.SortOrder)
		if err != nil {
			return fmt.Errorf("reorder analysis instruction: %w", err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("reorder analysis instruction rows affected: %w", err)
		}
		if rowsAffected == 0 {
			return fmt.Errorf("%w: %w", model.ErrAnalysisInstructionNotFound, sql.ErrNoRows)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reorder analysis instructions: %w", err)
	}

	return nil
}
