package call_folder

import (
	"context"
	"fmt"

	"calllens/monolit/internal/models"
	"calllens/monolit/internal/repository/converter"
	repoModel "calllens/monolit/internal/repository/models"
	"calllens/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) ReplaceInstructions(ctx context.Context, folderID uuid.UUID, instructionIDs []uuid.UUID, actorID uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace folder instructions: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `DELETE FROM call_folder_instructions WHERE folder_uuid=$1`, folderID); err != nil {
		return fmt.Errorf("clear folder instructions: %w", err)
	}
	for _, instructionID := range instructionIDs {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO call_folder_instructions(folder_uuid,instruction_uuid,created_by_user_uuid)
			VALUES($1,$2,$3)
		`, folderID, instructionID, actorID); err != nil {
			return fmt.Errorf("attach folder instruction: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit replace folder instructions: %w", err)
	}
	return nil
}

func (r *Repository) ListInstructions(ctx context.Context, folderID uuid.UUID) ([]models.AnalysisInstruction, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ai.instruction_uuid,ai.scope,ai.user_uuid,ai.company_uuid,ai.department_uuid,
		       ai.title,ai.original_filename,ai.file_path,ai.mime_type,ai.size_bytes,
		       ai.content_sha256,ai.sort_order,ai.is_active,ai.created_by_user_uuid,ai.created_at,ai.updated_at
		FROM call_folder_instructions cfi
		JOIN analysis_instructions ai ON ai.instruction_uuid=cfi.instruction_uuid
		WHERE cfi.folder_uuid=$1 AND ai.is_active=true
		ORDER BY ai.sort_order,ai.created_at
	`, folderID)
	if err != nil {
		return nil, fmt.Errorf("list folder instructions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]repoModel.AnalysisInstruction, 0)
	for rows.Next() {
		item, scanErr := scaner.ScanAnalysisInstruction(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan folder instruction: %w", scanErr)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate folder instructions: %w", err)
	}
	return converter.RepoAnalysisInstructionsToModels(items)
}

func (r *Repository) ListInstructionsForCall(ctx context.Context, callID uuid.UUID) ([]models.AnalysisInstruction, error) {
	var folderID uuid.UUID
	err := r.db.QueryRowContext(ctx, `
		SELECT folder_uuid FROM call_folder_assignments WHERE call_uuid=$1
	`, callID).Scan(&folderID)
	if err != nil {
		return []models.AnalysisInstruction{}, nil
	}
	return r.ListInstructions(ctx, folderID)
}
