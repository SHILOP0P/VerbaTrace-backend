package analysis_instruction

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
)

func (r *Repository) CountActive(ctx context.Context, input model.ListAnalysisInstructionsInput) (int, error) {
	query := `
	SELECT COUNT(*)
	FROM analysis_instructions
	WHERE is_active = true
	  AND scope = $1
	  AND (
	      ($1 = 'personal' AND user_uuid = $2)
	      OR
	      ($1 = 'company' AND company_uuid = $3 AND department_uuid IS NULL)
	      OR
	      ($1 = 'department' AND company_uuid = $3 AND department_uuid = $4)
	  )
	`

	var count int
	if err := r.db.QueryRowContext(
		ctx,
		query,
		input.Scope,
		input.UserUUID,
		input.CompanyUUID,
		input.DepartmentUUID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active analysis instructions: %w", err)
	}

	return count, nil
}
