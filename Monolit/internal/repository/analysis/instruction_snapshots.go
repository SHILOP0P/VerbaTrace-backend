package analysis

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"verbatrace/monolit/internal/models"
)

func (r *Repository) SaveInstructionSnapshots(ctx context.Context, analysisID uuid.UUID, instructions []models.AnalysisInstructionContent) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin instruction snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `DELETE FROM call_analysis_instruction_snapshots WHERE analysis_uuid=$1`, analysisID); err != nil {
		return err
	}
	for position, instruction := range instructions {
		var versionID uuid.UUID
		if err = tx.QueryRowContext(ctx, `SELECT instruction_version_uuid FROM analysis_instruction_versions WHERE instruction_uuid=$1 ORDER BY version DESC LIMIT 1`, instruction.ID).Scan(&versionID); err != nil {
			return fmt.Errorf("resolve instruction version: %w", err)
		}
		source := string(instruction.Scope)
		if source != "personal" && source != "company" && source != "department" {
			source = "explicit"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO call_analysis_instruction_snapshots(analysis_uuid,instruction_version_uuid,instruction_uuid,position,selection_source,title_snapshot,scope_snapshot,content_sha256,content_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, analysisID, versionID, instruction.ID, position, source, instruction.Title, string(instruction.Scope), instruction.ContentSHA256, instruction.Content); err != nil {
			return fmt.Errorf("insert instruction snapshot: %w", err)
		}
	}
	return tx.Commit()
}

const instructionSnapshotSelect = `SELECT s.analysis_uuid,a.call_uuid,s.instruction_uuid,s.instruction_version_uuid,v.version,s.position,s.title_snapshot,s.scope_snapshot,s.selection_source,s.content_sha256,%s,v.original_filename,(i.deleted_at IS NOT NULL),s.created_at FROM call_analysis_instruction_snapshots s JOIN call_analyses a ON a.analysis_uuid=s.analysis_uuid JOIN analysis_instruction_versions v ON v.instruction_version_uuid=s.instruction_version_uuid LEFT JOIN analysis_instructions i ON i.instruction_uuid=s.instruction_uuid`

func scanApplied(scanner interface{ Scan(...any) error }, item *models.AppliedInstruction) error {
	return scanner.Scan(&item.AnalysisUUID, &item.CallUUID, &item.InstructionUUID, &item.VersionUUID, &item.Version, &item.Position, &item.Title, &item.Scope, &item.SelectionSource, &item.ContentSHA256, &item.Content, &item.OriginalFilename, &item.InstructionDeleted, &item.CreatedAt)
}
func (r *Repository) ListInstructionSnapshots(ctx context.Context, analysisID uuid.UUID) ([]models.AppliedInstruction, error) {
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(instructionSnapshotSelect, "''")+` WHERE s.analysis_uuid=$1 ORDER BY s.position`, analysisID)
	if err != nil {
		return nil, fmt.Errorf("list instruction snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.AppliedInstruction, 0)
	for rows.Next() {
		var item models.AppliedInstruction
		if err = scanApplied(rows, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *Repository) GetInstructionSnapshot(ctx context.Context, analysisID, versionID uuid.UUID) (models.AppliedInstruction, error) {
	var item models.AppliedInstruction
	err := scanApplied(r.db.QueryRowContext(ctx, fmt.Sprintf(instructionSnapshotSelect, "s.content_snapshot")+` WHERE s.analysis_uuid=$1 AND s.instruction_version_uuid=$2`, analysisID, versionID), &item)
	if errors.Is(err, sql.ErrNoRows) {
		return item, models.ErrAnalysisInstructionNotFound
	}
	if err != nil {
		return item, fmt.Errorf("get instruction snapshot: %w", err)
	}
	return item, nil
}

func (r *Repository) InstructionSnapshotCallUUID(ctx context.Context, analysisID uuid.UUID) (uuid.UUID, error) {
	var callID uuid.UUID
	err := r.db.QueryRowContext(ctx, `SELECT call_uuid FROM call_analyses WHERE analysis_uuid=$1`, analysisID).Scan(&callID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, models.ErrAnalysisNotFound
	}
	return callID, err
}
