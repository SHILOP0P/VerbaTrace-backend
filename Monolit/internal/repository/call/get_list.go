package call

import (
	"context"
	"fmt"
	"strings"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"
	repoModel "verbatrace/monolit/internal/repository/models"
	"verbatrace/monolit/internal/repository/scaner"

	"github.com/google/uuid"
)

func (r *Repository) List(ctx context.Context, userID uuid.UUID) ([]model.Call, error) {
	var calls []repoModel.Call

	qList := fmt.Sprintf(`
	SELECT c.call_uuid,
	       title,
	       status,
	       audio_path,
	       asr_cache_path,
	       original_filename,
	       mime_type,
	       size_bytes,
	       duration_seconds,
	       uploaded_by_user_uuid,
	       company_uuid,
	       department_uuid,
	       visibility_scope,
	       skip_custom_instructions,
	       EXISTS (SELECT 1 FROM ingest_items i JOIN developer_applications a USING(application_uuid) WHERE i.ingest_item_uuid=c.ingest_item_uuid AND a.environment='sandbox') AS is_test,
	       created_at
	FROM calls c
	WHERE %s
	ORDER BY created_at DESC
	`, visibleToUserCondition("c", "$1"))

	rows, err := r.db.QueryContext(ctx, qList, userID)
	if err != nil {
		return nil, fmt.Errorf("list calls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var call repoModel.Call
		call, err = scaner.ScanCall(rows)
		if err != nil {
			return nil, fmt.Errorf("list calls: %w", err)
		}
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list calls: %w", err)
	}

	return converter.RepoCallsToModels(calls)
}

func (r *Repository) ListFiltered(ctx context.Context, input model.ListCallsInput) (model.ListCallsResult, error) {
	where, args := buildListFilters(input)
	limitParam := len(args) + 1
	args = append(args, input.Limit)
	offsetParam := len(args) + 1
	args = append(args, input.Offset)

	qList := fmt.Sprintf(`
	SELECT c.call_uuid,
	       c.title,
	       c.status,
	       c.audio_path,
	       c.asr_cache_path,
	       c.original_filename,
	       c.mime_type,
	       c.size_bytes,
	       c.duration_seconds,
	       c.uploaded_by_user_uuid,
	       c.company_uuid,
	       c.department_uuid,
	       c.visibility_scope,
	       c.skip_custom_instructions,
	       EXISTS (SELECT 1 FROM ingest_items i JOIN developer_applications a USING(application_uuid) WHERE i.ingest_item_uuid=c.ingest_item_uuid AND a.environment='sandbox') AS is_test,
	       c.created_at,
	       COUNT(*) OVER() AS total
	FROM calls c
	WHERE %s
	ORDER BY c.created_at DESC
	LIMIT $%d OFFSET $%d
	`, where, limitParam, offsetParam)

	rows, err := r.db.QueryContext(ctx, qList, args...)
	if err != nil {
		return model.ListCallsResult{}, fmt.Errorf("list filtered calls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var calls []repoModel.Call
	total := 0
	for rows.Next() {
		var call repoModel.Call
		err = rows.Scan(
			&call.ID,
			&call.Title,
			&call.Status,
			&call.AudioPath,
			&call.ASRCachePath,
			&call.OriginalFilename,
			&call.MimeType,
			&call.SizeBytes,
			&call.DurationSeconds,
			&call.UploadedByUserUUID,
			&call.CompanyUUID,
			&call.DepartmentUUID,
			&call.VisibilityScope,
			&call.SkipCustomInstructions,
			&call.IsTest,
			&call.CreatedAt,
			&total,
		)
		if err != nil {
			return model.ListCallsResult{}, fmt.Errorf("list filtered calls: %w", err)
		}
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return model.ListCallsResult{}, fmt.Errorf("list filtered calls: %w", err)
	}

	converted, err := converter.RepoCallsToModels(calls)
	if err != nil {
		return model.ListCallsResult{}, fmt.Errorf("list filtered calls: %w", err)
	}

	if len(converted) == 0 && input.Offset > 0 {
		total, err = r.countFilteredCalls(ctx, input)
		if err != nil {
			return model.ListCallsResult{}, err
		}
	}

	return model.ListCallsResult{
		Items:  converted,
		Total:  total,
		Limit:  input.Limit,
		Offset: input.Offset,
	}, nil
}

func (r *Repository) countFilteredCalls(ctx context.Context, input model.ListCallsInput) (int, error) {
	where, args := buildListFilters(input)
	qCount := fmt.Sprintf(`SELECT COUNT(*) FROM calls c WHERE %s`, where)

	var total int
	if err := r.db.QueryRowContext(ctx, qCount, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count filtered calls: %w", err)
	}

	return total, nil
}

func (r *Repository) GetFilterOptions(ctx context.Context, input model.CallFilterOptionsInput) (model.CallFilterOptions, error) {
	where, args := buildFilterOptionsFilters(input)
	qList := fmt.Sprintf(`
	SELECT DISTINCT u.user_uuid,
	       p.full_name,
	       p.full_surname,
	       p.username
	FROM calls c
	JOIN users u ON u.user_uuid = c.uploaded_by_user_uuid
	JOIN user_profiles p ON p.user_uuid = u.user_uuid
	WHERE %s
	ORDER BY p.full_surname, p.full_name, p.username
	`, where)

	rows, err := r.db.QueryContext(ctx, qList, args...)
	if err != nil {
		return model.CallFilterOptions{}, fmt.Errorf("get call filter options: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var managers []model.CallFilterUser
	for rows.Next() {
		var manager model.CallFilterUser
		if err := rows.Scan(&manager.ID, &manager.FullName, &manager.FullSurname, &manager.Username); err != nil {
			return model.CallFilterOptions{}, fmt.Errorf("get call filter options: %w", err)
		}
		managers = append(managers, manager)
	}
	if err := rows.Err(); err != nil {
		return model.CallFilterOptions{}, fmt.Errorf("get call filter options: %w", err)
	}

	return model.CallFilterOptions{
		Statuses: []model.CallStatus{
			model.CallStatusNew,
			model.CallStatusProcessing,
			model.CallStatusTranscribed,
			model.CallStatusAnalyzed,
			model.CallStatusFailed,
		},
		Scopes: []model.CallVisibilityScope{
			model.CallVisibilityScopePersonal,
			model.CallVisibilityScopeCompany,
			model.CallVisibilityScopeDepartment,
		},
		Managers: managers,
	}, nil
}

func buildListFilters(input model.ListCallsInput) (string, []any) {
	args := []any{input.UserID}
	conditions := []string{visibleToUserCondition("c", "$1")}

	if input.Q != "" {
		args = append(args, "%"+strings.ToLower(input.Q)+"%")
		conditions = append(conditions, fmt.Sprintf("(LOWER(c.title) LIKE $%d OR LOWER(c.original_filename) LIKE $%d)", len(args), len(args)))
	}
	if input.Status != "" {
		args = append(args, string(input.Status))
		conditions = append(conditions, fmt.Sprintf("c.status = $%d", len(args)))
	}
	if input.VisibilityScope != "" {
		args = append(args, string(input.VisibilityScope))
		conditions = append(conditions, fmt.Sprintf("c.visibility_scope = $%d", len(args)))
	}
	if input.CompanyUUID.Valid {
		args = append(args, input.CompanyUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("c.company_uuid = $%d", len(args)))
	}
	if input.DepartmentUUID.Valid {
		args = append(args, input.DepartmentUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("c.department_uuid = $%d", len(args)))
	}
	if input.UploadedByUserUUID.Valid {
		args = append(args, input.UploadedByUserUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("c.uploaded_by_user_uuid = $%d", len(args)))
	}
	if input.From != nil {
		args = append(args, *input.From)
		conditions = append(conditions, fmt.Sprintf("c.created_at >= $%d", len(args)))
	}
	if input.To != nil {
		args = append(args, *input.To)
		conditions = append(conditions, fmt.Sprintf("c.created_at <= $%d", len(args)))
	}
	if input.FolderUUID.Valid {
		args = append(args, input.FolderUUID.UUID)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM call_folder_assignments cfa
			JOIN call_folders cf ON cf.folder_uuid = cfa.folder_uuid
			WHERE cfa.call_uuid = c.call_uuid
			  AND cf.folder_uuid = $%d
			  AND cf.deleted_at IS NULL
		)`, len(args)))
	}

	return strings.Join(conditions, " AND "), args
}

func buildFilterOptionsFilters(input model.CallFilterOptionsInput) (string, []any) {
	args := []any{input.UserID}
	conditions := []string{visibleToUserCondition("c", "$1"), "c.uploaded_by_user_uuid IS NOT NULL"}

	if input.CompanyUUID.Valid {
		args = append(args, input.CompanyUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("c.company_uuid = $%d", len(args)))
	}
	if input.DepartmentUUID.Valid {
		args = append(args, input.DepartmentUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("c.department_uuid = $%d", len(args)))
	}

	return strings.Join(conditions, " AND "), args
}
