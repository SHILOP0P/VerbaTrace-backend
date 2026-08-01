package analysis_instruction

import (
	"context"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) Create(ctx context.Context, instruction model.AnalysisInstruction) (model.AnalysisInstruction, error) {
	repoInstruction, err := converter.ModelAnalysisInstructionToRepoAnalysisInstruction(instruction)
	if err != nil {
		return model.AnalysisInstruction{}, fmt.Errorf("convert analysis instruction to repo: %w", err)
	}

	query := `
	INSERT INTO analysis_instructions (
		instruction_uuid,
		scope,
		user_uuid,
		company_uuid,
		department_uuid,
		title,
		original_filename,
		file_path,
		mime_type,
		size_bytes,
		content_sha256,
		sort_order,
		is_active,
		created_by_user_uuid,
		created_at,
		updated_at
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	RETURNING ` + analysisInstructionReturningColumns

	row := r.db.QueryRowContext(
		ctx,
		query,
		repoInstruction.ID,
		repoInstruction.Scope,
		repoInstruction.UserUUID,
		repoInstruction.CompanyUUID,
		repoInstruction.DepartmentUUID,
		repoInstruction.Title,
		repoInstruction.OriginalFilename,
		repoInstruction.FilePath,
		repoInstruction.MimeType,
		repoInstruction.SizeBytes,
		repoInstruction.ContentSHA256,
		repoInstruction.SortOrder,
		repoInstruction.IsActive,
		repoInstruction.CreatedByUserUUID,
		repoInstruction.CreatedAt,
		repoInstruction.UpdatedAt,
	)

	var created repoModel.AnalysisInstruction
	created, err = scaner.ScanAnalysisInstruction(row)
	if err != nil {
		return model.AnalysisInstruction{}, fmt.Errorf("create analysis instruction: %w", err)
	}

	return converter.RepoAnalysisInstructionToModel(created)
}
