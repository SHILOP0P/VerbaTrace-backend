package analysis_instruction

import (
	"context"
	"fmt"
	"strings"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) List(ctx context.Context, input model.ListAnalysisInstructionsInput) ([]model.AnalysisInstruction, error) {
	args := []any{
		input.Scope,
		input.UserUUID,
		input.CompanyUUID,
		input.DepartmentUUID,
	}
	query := `
	SELECT ` + analysisInstructionReturningColumns + `
	FROM analysis_instructions
	WHERE scope = $1
	  AND (
	      ($1 = 'personal' AND user_uuid = $2)
	      OR
	      ($1 = 'company' AND company_uuid = $3 AND department_uuid IS NULL)
	      OR
	      ($1 = 'department' AND company_uuid = $3 AND department_uuid = $4)
	  )
	`
	if !input.IncludeInactive {
		query += "\n  AND is_active = true"
	}
	if strings.TrimSpace(input.Query) != "" {
		args = append(args, "%"+strings.TrimSpace(input.Query)+"%")
		query += fmt.Sprintf("\n  AND (title ILIKE $%d OR original_filename ILIKE $%d)", len(args), len(args))
	}
	query += "\nORDER BY sort_order ASC, created_at ASC"
	if input.Limit > 0 {
		args = append(args, input.Limit)
		query += fmt.Sprintf("\nLIMIT $%d", len(args))
	}
	if input.Offset > 0 {
		args = append(args, input.Offset)
		query += fmt.Sprintf("\nOFFSET $%d", len(args))
	}

	rows, err := r.db.QueryContext(
		ctx,
		query,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list analysis instructions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	instructions := make([]repoModel.AnalysisInstruction, 0)
	for rows.Next() {
		instruction, err := scaner.ScanAnalysisInstruction(rows)
		if err != nil {
			return nil, fmt.Errorf("scan analysis instruction: %w", err)
		}
		instructions = append(instructions, instruction)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate analysis instructions: %w", err)
	}

	return converter.RepoAnalysisInstructionsToModels(instructions)
}
