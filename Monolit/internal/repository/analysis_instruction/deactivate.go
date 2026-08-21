package analysis_instruction

import (
	"context"
	"database/sql"
	"fmt"

	model "verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) Deactivate(ctx context.Context, id uuid.UUID) error {
	query := `
	UPDATE analysis_instructions
	SET is_active = false,
	    deleted_at = now(),
	    purge_state = 'eligible',
	    purge_after = now() + interval '7 days',
	    updated_at = now()
	WHERE instruction_uuid = $1
	  AND is_active = true
	`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("deactivate analysis instruction: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("deactivate analysis instruction rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("%w: %w", model.ErrAnalysisInstructionNotFound, sql.ErrNoRows)
	}

	return nil
}
