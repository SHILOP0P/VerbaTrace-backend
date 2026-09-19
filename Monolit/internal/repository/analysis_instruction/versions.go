package analysis_instruction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const instructionVersionColumns = `instruction_version_uuid,instruction_uuid,version,title_snapshot,scope_snapshot,original_filename,mime_type,size_bytes,file_path,status,created_by_user_uuid,created_at,COALESCE(published_at,created_at)`

func (r *Repository) ListVersions(ctx context.Context, id uuid.UUID) ([]models.AnalysisInstructionVersion, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+instructionVersionColumns+` FROM analysis_instruction_versions WHERE instruction_uuid=$1 ORDER BY version DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("list instruction versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.AnalysisInstructionVersion, 0)
	for rows.Next() {
		item, scanErr := scanInstructionVersion(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate instruction versions: %w", err)
	}
	return items, nil
}

func (r *Repository) GetVersion(ctx context.Context, id uuid.UUID, versionID uuid.UUID) (models.AnalysisInstructionVersion, error) {
	item, err := scanInstructionVersion(r.db.QueryRowContext(ctx, `SELECT `+instructionVersionColumns+` FROM analysis_instruction_versions WHERE instruction_uuid=$1 AND instruction_version_uuid=$2`, id, versionID))
	if errors.Is(err, sql.ErrNoRows) {
		return models.AnalysisInstructionVersion{}, models.ErrAnalysisInstructionNotFound
	}
	return item, err
}

// ReadableVersion names the stored version whose file an analysis is about to
// read. The instruction row only says what is current now; another save can land
// between reading the file and writing the snapshot, so the version is fixed at
// read time by the file it points to. A rename keeps the file, so the title
// breaks the tie between such versions.
func (r *Repository) ReadableVersion(ctx context.Context, instruction models.AnalysisInstruction) (models.InstructionVersionText, error) {
	var item models.InstructionVersionText
	var text sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT instruction_version_uuid, content_text
		FROM analysis_instruction_versions
		WHERE instruction_uuid=$1 AND file_path=$2 AND content_sha256=$3
		ORDER BY (title_snapshot=$4) DESC, version DESC
		LIMIT 1`, instruction.ID, instruction.FilePath, instruction.ContentSHA256, instruction.Title).Scan(&item.VersionID, &text)
	if errors.Is(err, sql.ErrNoRows) {
		return item, models.ErrAnalysisInstructionNotFound
	}
	if err != nil {
		return item, fmt.Errorf("resolve readable instruction version: %w", err)
	}
	if text.Valid {
		item.Text = &text.String
	}
	return item, nil
}

// SaveVersionText keeps the first extraction. A version is immutable, so a
// second writer would store the same text and is simply ignored.
func (r *Repository) SaveVersionText(ctx context.Context, versionID uuid.UUID, text string) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE analysis_instruction_versions SET content_text=$2 WHERE instruction_version_uuid=$1 AND content_text IS NULL`, versionID, text); err != nil {
		return fmt.Errorf("save instruction version text: %w", err)
	}
	return nil
}

type versionScanner interface{ Scan(...any) error }

func scanInstructionVersion(row versionScanner) (models.AnalysisInstructionVersion, error) {
	var item models.AnalysisInstructionVersion
	var creator uuid.NullUUID
	if err := row.Scan(&item.ID, &item.InstructionID, &item.Version, &item.Title, &item.Scope, &item.OriginalFilename, &item.MimeType, &item.SizeBytes, &item.FilePath, &item.Status, &creator, &item.CreatedAt, &item.PublishedAt); err != nil {
		return item, err
	}
	if creator.Valid {
		item.CreatedByUserUUID = creator.UUID
	}
	return item, nil
}
