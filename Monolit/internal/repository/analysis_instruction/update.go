package analysis_instruction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"
)

func (r *Repository) Update(ctx context.Context, input model.UpdateAnalysisInstructionRepositoryInput) (model.AnalysisInstruction, error) {
	query := `
	UPDATE analysis_instructions
	SET title = COALESCE($2, title),
	    is_active = COALESCE($3, is_active),
	    sort_order = COALESCE($4, sort_order),
	    file_path = COALESCE($5, file_path),
	    mime_type = COALESCE($6, mime_type),
	    size_bytes = COALESCE($7, size_bytes),
	    content_sha256 = COALESCE($8, content_sha256),
	    original_filename = CASE WHEN $9::text IS NULL THEN original_filename ELSE $9 END,
	    updated_at = now()
	WHERE instruction_uuid = $1
	RETURNING ` + analysisInstructionReturningColumns

	row := r.db.QueryRowContext(
		ctx,
		query,
		input.ID,
		input.Title,
		input.IsActive,
		input.SortOrder,
		input.FilePath,
		input.MimeType,
		input.SizeBytes,
		input.ContentSHA256,
		input.OriginalFilename,
	)

	var repoInstruction repoModel.AnalysisInstruction
	repoInstruction, err := scaner.ScanAnalysisInstruction(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.AnalysisInstruction{}, model.ErrAnalysisInstructionNotFound
		}
		return model.AnalysisInstruction{}, fmt.Errorf("update analysis instruction: %w", err)
	}

	return converter.RepoAnalysisInstructionToModel(repoInstruction)
}
