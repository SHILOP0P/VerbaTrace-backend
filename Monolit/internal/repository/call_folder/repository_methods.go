package call_folder

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/call"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"

	"github.com/google/uuid"
)

const folderSelect = `
SELECT f.folder_uuid,
       f.scope,
       f.user_uuid,
       f.company_uuid,
       f.department_uuid,
       f.name,
       f.description,
       f.color,
       COUNT(ac.call_uuid)::int AS calls_count,
       f.created_by_user_uuid,
       f.created_at,
       f.updated_at,
       f.deleted_at
FROM call_folders f
LEFT JOIN call_folder_assignments a ON a.folder_uuid = f.folder_uuid
LEFT JOIN calls ac ON ac.call_uuid = a.call_uuid AND ac.deleted_at IS NULL
`

func (r *Repository) Create(ctx context.Context, folder models.CallFolder) (models.CallFolder, error) {
	query := `
INSERT INTO call_folders (
    folder_uuid, scope, user_uuid, company_uuid, department_uuid, name, description, color, created_by_user_uuid
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	if _, err := r.db.ExecContext(ctx, query,
		folder.ID,
		string(folder.Scope),
		nullUUID(folder.UserUUID),
		nullUUID(folder.CompanyUUID),
		nullUUID(folder.DepartmentUUID),
		folder.Name,
		nullString(folder.Description),
		nullString(folder.Color),
		folder.CreatedByUserUUID,
	); err != nil {
		return models.CallFolder{}, fmt.Errorf("create call folder: %w", err)
	}

	return r.GetByUUID(ctx, folder.ID)
}

func (r *Repository) GetByUUID(ctx context.Context, id uuid.UUID) (models.CallFolder, error) {
	query := folderSelect + `
WHERE f.folder_uuid = $1 AND f.deleted_at IS NULL
GROUP BY f.folder_uuid`
	row := r.db.QueryRowContext(ctx, query, id)
	folder, err := scanFolder(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.CallFolder{}, models.ErrCallFolderNotFound
		}
		return models.CallFolder{}, fmt.Errorf("get call folder: %w", err)
	}
	return folder, nil
}

func (r *Repository) GetVisibleByUUID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.CallFolder, error) {
	query := folderSelect + fmt.Sprintf(`
WHERE f.folder_uuid = $1
  AND f.deleted_at IS NULL
  AND %s
GROUP BY f.folder_uuid`, visibleFolderCondition("$2"))
	row := r.db.QueryRowContext(ctx, query, id, userID)
	folder, err := scanFolder(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.CallFolder{}, models.ErrCallFolderNotFound
		}
		return models.CallFolder{}, fmt.Errorf("get visible call folder: %w", err)
	}
	return folder, nil
}

func (r *Repository) List(ctx context.Context, input models.ListCallFoldersInput) (models.ListCallFoldersResult, error) {
	where, args := buildFolderListFilters(input)
	limitParam := len(args) + 1
	args = append(args, input.Limit)
	offsetParam := len(args) + 1
	args = append(args, input.Offset)

	query := folderSelect + fmt.Sprintf(`
WHERE %s
GROUP BY f.folder_uuid
ORDER BY f.created_at DESC
LIMIT $%d OFFSET $%d`, where, limitParam, offsetParam)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return models.ListCallFoldersResult{}, fmt.Errorf("list call folders: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := []models.CallFolder{}
	for rows.Next() {
		folder, err := scanFolder(rows)
		if err != nil {
			return models.ListCallFoldersResult{}, fmt.Errorf("list call folders: %w", err)
		}
		items = append(items, folder)
	}
	if err := rows.Err(); err != nil {
		return models.ListCallFoldersResult{}, fmt.Errorf("list call folders: %w", err)
	}

	total, err := r.countFolders(ctx, where, args[:len(args)-2])
	if err != nil {
		return models.ListCallFoldersResult{}, err
	}
	return models.ListCallFoldersResult{Items: items, Total: total, Limit: input.Limit, Offset: input.Offset}, nil
}

func (r *Repository) countFolders(ctx context.Context, where string, args []any) (int, error) {
	query := fmt.Sprintf(`SELECT COUNT(*) FROM call_folders f WHERE %s`, where)
	var total int
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count call folders: %w", err)
	}
	return total, nil
}

func (r *Repository) Update(ctx context.Context, input models.UpdateCallFolderInput) (models.CallFolder, error) {
	query := `
WITH updated AS (
    UPDATE call_folders
    SET name = COALESCE($2, name),
        description = $3,
        color = $4,
        updated_at = now()
    WHERE folder_uuid = $1 AND deleted_at IS NULL
    RETURNING folder_uuid
)
` + folderSelect + `
JOIN updated ON updated.folder_uuid = f.folder_uuid
GROUP BY f.folder_uuid`
	row := r.db.QueryRowContext(ctx, query, input.FolderUUID, nullString(input.Name), nullString(input.Description), nullString(input.Color))
	folder, err := scanFolder(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.CallFolder{}, models.ErrCallFolderNotFound
		}
		return models.CallFolder{}, fmt.Errorf("update call folder: %w", err)
	}
	return folder, nil
}

func (r *Repository) SoftDelete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `UPDATE call_folders SET deleted_at = now(), updated_at = now() WHERE folder_uuid = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("delete call folder: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete call folder: %w", err)
	}
	if affected == 0 {
		return models.ErrCallFolderNotFound
	}
	return nil
}

func (r *Repository) AssignCall(ctx context.Context, input models.AssignCallToFolderInput) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO call_folder_assignments (folder_uuid, call_uuid, assigned_by_user_uuid)
VALUES ($1, $2, $3)
ON CONFLICT (folder_uuid, call_uuid) DO NOTHING`, input.FolderUUID, input.CallUUID, input.UserID)
	if err != nil {
		return fmt.Errorf("assign call folder: %w", err)
	}
	return nil
}

func (r *Repository) RemoveCall(ctx context.Context, input models.RemoveCallFromFolderInput) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM call_folder_assignments WHERE folder_uuid = $1 AND call_uuid = $2`, input.FolderUUID, input.CallUUID)
	if err != nil {
		return fmt.Errorf("remove call folder: %w", err)
	}
	return nil
}

// ListFolderCalls returns only the calls the caller may see anyway: a folder is
// a shortcut to calls, never a way around call visibility.
func (r *Repository) ListFolderCalls(ctx context.Context, input models.ListFolderCallsInput) (models.ListCallsResult, error) {
	limit := input.Limit
	offset := input.Offset
	query := fmt.Sprintf(`
SELECT c.call_uuid,
       c.title,
       c.status,
       c.audio_path,
       c.original_filename,
       c.mime_type,
       c.size_bytes,
       c.duration_seconds,
       c.uploaded_by_user_uuid,
       c.company_uuid,
       c.department_uuid,
       c.visibility_scope,
       c.skip_custom_instructions,
       c.transcription_only,
       %s AS is_test,
       c.created_at,
       COUNT(*) OVER() AS total
FROM calls c
JOIN call_folder_assignments a ON a.call_uuid = c.call_uuid
JOIN call_folders f ON f.folder_uuid = a.folder_uuid
WHERE f.folder_uuid = $1
  AND f.deleted_at IS NULL
  AND %s
ORDER BY a.created_at DESC
LIMIT $3 OFFSET $4`, call.IsTestExpr("c"), call.VisibleToUserCondition("c", "$2"))

	rows, err := r.db.QueryContext(ctx, query, input.FolderUUID, input.UserID, limit, offset)
	if err != nil {
		return models.ListCallsResult{}, fmt.Errorf("list folder calls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	calls := []repoModel.Call{}
	total := 0
	for rows.Next() {
		var call repoModel.Call
		err := rows.Scan(
			&call.ID,
			&call.Title,
			&call.Status,
			&call.AudioPath,
			&call.OriginalFilename,
			&call.MimeType,
			&call.SizeBytes,
			&call.DurationSeconds,
			&call.UploadedByUserUUID,
			&call.CompanyUUID,
			&call.DepartmentUUID,
			&call.VisibilityScope,
			&call.SkipCustomInstructions,
			&call.TranscriptionOnly,
			&call.IsTest,
			&call.CreatedAt,
			&total,
		)
		if err != nil {
			return models.ListCallsResult{}, fmt.Errorf("list folder calls: %w", err)
		}
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return models.ListCallsResult{}, fmt.Errorf("list folder calls: %w", err)
	}
	converted, err := converter.RepoCallsToModels(calls)
	if err != nil {
		return models.ListCallsResult{}, fmt.Errorf("list folder calls: %w", err)
	}
	return models.ListCallsResult{Items: converted, Total: total, Limit: limit, Offset: offset}, nil
}

func buildFolderListFilters(input models.ListCallFoldersInput) (string, []any) {
	args := []any{input.UserID}
	conditions := []string{"f.deleted_at IS NULL", visibleFolderCondition("$1")}
	if input.Scope != "" {
		args = append(args, string(input.Scope))
		conditions = append(conditions, fmt.Sprintf("f.scope = $%d", len(args)))
	}
	if input.CompanyUUID.Valid {
		args = append(args, input.CompanyUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("f.company_uuid = $%d", len(args)))
	}
	if input.DepartmentUUID.Valid {
		args = append(args, input.DepartmentUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("f.department_uuid = $%d", len(args)))
	}
	if input.Q != "" {
		args = append(args, "%"+strings.ToLower(input.Q)+"%")
		conditions = append(conditions, fmt.Sprintf("(LOWER(f.name) LIKE $%d OR LOWER(COALESCE(f.description, '')) LIKE $%d)", len(args), len(args)))
	}
	return strings.Join(conditions, " AND "), args
}

// visibleFolderCondition mirrors call visibility: a regular employee sees only
// folders that hold their own calls, a department leader sees the folders of
// their department, and the owner with the deputy see everything in the company.
func visibleFolderCondition(userParam string) string {
	return fmt.Sprintf(`(
    (f.scope = 'personal' AND f.user_uuid = %s)
    OR (
        f.company_uuid IS NOT NULL
        AND EXISTS (
            SELECT 1 FROM company_members cm
            WHERE cm.company_uuid = f.company_uuid
              AND cm.user_uuid = %s
              AND cm.role IN ('company_manager','company_deputy')
              AND cm.status = 'active'
        )
    )
    OR (
        f.scope = 'department'
        AND EXISTS (
            SELECT 1 FROM department_members dm
            WHERE dm.department_uuid = f.department_uuid
              AND dm.user_uuid = %s
              AND dm.role = 'department_leader'
              AND dm.status = 'active'
        )
    )
    OR EXISTS (
        SELECT 1
        FROM call_folder_assignments a
        JOIN calls c ON c.call_uuid = a.call_uuid
        WHERE a.folder_uuid = f.folder_uuid
          AND c.uploaded_by_user_uuid = %s
          AND c.deleted_at IS NULL
    )
)`, userParam, userParam, userParam, userParam)
}
