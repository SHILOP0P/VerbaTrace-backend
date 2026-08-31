package call

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

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
	       c.occurred_at,
	       COALESCE(c.occurred_at,c.created_at) AS display_time,
	       CASE WHEN c.occurred_at IS NULL THEN 'upload_fallback' ELSE 'source' END AS time_source,
	       ic.provider,
	       c.integration_connection_uuid,
	       ii.external_call_id,
	       ii.created_at AS imported_at,
	       ii.error_code,
	       EXISTS (SELECT 1 FROM call_analyses ca WHERE ca.call_uuid=c.call_uuid AND ca.status='done') AS has_analysis,
	       EXISTS (SELECT 1 FROM call_actions aa WHERE aa.call_uuid=c.call_uuid) AS has_actions,
	       COUNT(*) OVER() AS total
	FROM calls c
	LEFT JOIN ingest_items ii ON ii.ingest_item_uuid=c.ingest_item_uuid
	LEFT JOIN integration_connections ic ON ic.connection_uuid=c.integration_connection_uuid
	WHERE %s
	ORDER BY %s %s, c.call_uuid %s
	LIMIT $%d OFFSET $%d
	`, where, callSortExpression(input.Sort), callSortOrder(input.Order), callSortOrder(input.Order), limitParam, offsetParam)

	rows, err := r.db.QueryContext(ctx, qList, args...)
	if err != nil {
		return model.ListCallsResult{}, fmt.Errorf("list filtered calls: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var calls []model.Call
	total := 0
	for rows.Next() {
		var call repoModel.Call
		var occurredAt sql.NullTime
		var sourceProvider, externalCallID, ingestErrorCode sql.NullString
		var connectionID uuid.NullUUID
		var importedAt sql.NullTime
		var displayTime time.Time
		var timeSource string
		var hasAnalysis, hasActions bool
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
			&occurredAt,
			&displayTime,
			&timeSource,
			&sourceProvider,
			&connectionID,
			&externalCallID,
			&importedAt,
			&ingestErrorCode,
			&hasAnalysis,
			&hasActions,
			&total,
		)
		if err != nil {
			return model.ListCallsResult{}, fmt.Errorf("list filtered calls: %w", err)
		}
		converted, convertErr := converter.RepoCallToModel(call)
		if convertErr != nil {
			return model.ListCallsResult{}, fmt.Errorf("list filtered calls: %w", convertErr)
		}
		converted.OccurredAt = nullableTime(occurredAt)
		converted.DisplayTime = displayTime
		converted.TimeSource = timeSource
		converted.SourceProvider = nullableString(sourceProvider)
		converted.IntegrationConnectionUUID = connectionID
		converted.ExternalCallID = nullableString(externalCallID)
		converted.ImportedAt = nullableTime(importedAt)
		converted.IngestErrorCode = nullableString(ingestErrorCode)
		converted.HasAnalysis = hasAnalysis
		converted.HasActions = hasActions
		calls = append(calls, converted)
	}
	if err := rows.Err(); err != nil {
		return model.ListCallsResult{}, fmt.Errorf("list filtered calls: %w", err)
	}

	if len(calls) == 0 && input.Offset > 0 {
		total, err = r.countFilteredCalls(ctx, input)
		if err != nil {
			return model.ListCallsResult{}, err
		}
	}

	result := model.ListCallsResult{
		Items:  calls,
		Total:  total,
		Limit:  input.Limit,
		Offset: input.Offset,
	}
	if len(calls) > 0 && input.Offset+len(calls) < total {
		last := calls[len(calls)-1]
		result.NextCursor = cursorForCall(last, input.Sort, input.Order)
	}
	return result, nil
}

func (r *Repository) countFilteredCalls(ctx context.Context, input model.ListCallsInput) (int, error) {
	where, args := buildListFilters(input)
	qCount := fmt.Sprintf(`SELECT COUNT(*) FROM calls c
		LEFT JOIN ingest_items ii ON ii.ingest_item_uuid=c.ingest_item_uuid
		LEFT JOIN integration_connections ic ON ic.connection_uuid=c.integration_connection_uuid
		WHERE %s`, where)

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
	if err := rows.Close(); err != nil {
		return model.CallFilterOptions{}, fmt.Errorf("close call filter manager options: %w", err)
	}

	connectionRows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
	SELECT DISTINCT ic.connection_uuid, ic.name, ic.provider
	FROM calls c
	JOIN integration_connections ic ON ic.connection_uuid = c.integration_connection_uuid
	WHERE %s
	ORDER BY ic.name, ic.connection_uuid
	`, where), args...)
	if err != nil {
		return model.CallFilterOptions{}, fmt.Errorf("get call connection filter options: %w", err)
	}
	defer func() { _ = connectionRows.Close() }()

	connections := make([]model.CallFilterConnection, 0)
	for connectionRows.Next() {
		var connection model.CallFilterConnection
		if err := connectionRows.Scan(&connection.ID, &connection.Name, &connection.Provider); err != nil {
			return model.CallFilterOptions{}, fmt.Errorf("scan call connection filter option: %w", err)
		}
		connections = append(connections, connection)
	}
	if err := connectionRows.Err(); err != nil {
		return model.CallFilterOptions{}, fmt.Errorf("get call connection filter options: %w", err)
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
		Managers:    managers,
		Connections: connections,
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
	if len(input.Statuses) > 0 {
		values := make([]string, len(input.Statuses))
		for i, status := range input.Statuses {
			values[i] = string(status)
		}
		args = append(args, values)
		conditions = append(conditions, fmt.Sprintf("c.status = ANY($%d)", len(args)))
	}
	if input.VisibilityScope != "" {
		args = append(args, string(input.VisibilityScope))
		conditions = append(conditions, fmt.Sprintf("c.visibility_scope = $%d", len(args)))
	}
	if len(input.VisibilityScopes) > 0 {
		values := make([]string, len(input.VisibilityScopes))
		for i, scope := range input.VisibilityScopes {
			values[i] = string(scope)
		}
		args = append(args, values)
		conditions = append(conditions, fmt.Sprintf("c.visibility_scope = ANY($%d)", len(args)))
	}
	if input.CompanyUUID.Valid {
		args = append(args, input.CompanyUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("c.company_uuid = $%d", len(args)))
	}
	if input.DepartmentUUID.Valid {
		args = append(args, input.DepartmentUUID.UUID)
		conditions = append(conditions, fmt.Sprintf("c.department_uuid = $%d", len(args)))
	}
	if len(input.DepartmentUUIDs) > 0 {
		args = append(args, input.DepartmentUUIDs)
		conditions = append(conditions, fmt.Sprintf("c.department_uuid = ANY($%d)", len(args)))
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
	if input.OccurredFrom != nil {
		args = append(args, *input.OccurredFrom)
		if input.IncludeUploadFallback {
			conditions = append(conditions, fmt.Sprintf("COALESCE(c.occurred_at,c.created_at) >= $%d", len(args)))
		} else {
			conditions = append(conditions, fmt.Sprintf("c.occurred_at >= $%d", len(args)))
		}
	}
	if input.OccurredTo != nil {
		args = append(args, *input.OccurredTo)
		if input.IncludeUploadFallback {
			conditions = append(conditions, fmt.Sprintf("COALESCE(c.occurred_at,c.created_at) < $%d", len(args)))
		} else {
			conditions = append(conditions, fmt.Sprintf("c.occurred_at < $%d", len(args)))
		}
	}
	if input.ImportedFrom != nil {
		args = append(args, *input.ImportedFrom)
		conditions = append(conditions, fmt.Sprintf("ii.created_at >= $%d", len(args)))
	}
	if input.ImportedTo != nil {
		args = append(args, *input.ImportedTo)
		conditions = append(conditions, fmt.Sprintf("ii.created_at < $%d", len(args)))
	}
	if input.DurationMinSeconds != nil {
		args = append(args, *input.DurationMinSeconds)
		conditions = append(conditions, fmt.Sprintf("c.duration_seconds >= $%d", len(args)))
	}
	if input.DurationMaxSeconds != nil {
		args = append(args, *input.DurationMaxSeconds)
		conditions = append(conditions, fmt.Sprintf("c.duration_seconds <= $%d", len(args)))
	}
	if input.SourceProvider != "" {
		if input.SourceProvider == "manual" {
			conditions = append(conditions, "c.integration_connection_uuid IS NULL")
		} else {
			args = append(args, input.SourceProvider)
			conditions = append(conditions, fmt.Sprintf("ic.provider = $%d", len(args)))
		}
	}
	if len(input.ConnectionUUIDs) > 0 {
		args = append(args, input.ConnectionUUIDs)
		conditions = append(conditions, fmt.Sprintf("c.integration_connection_uuid = ANY($%d)", len(args)))
	}
	if len(input.ParticipantUserUUIDs) > 0 {
		args = append(args, input.ParticipantUserUUIDs)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM integration_call_participants cp
			WHERE cp.call_uuid=c.call_uuid AND cp.internal_user_uuid=ANY($%d)
		)`, len(args)))
	}
	if input.HasAnalysis != nil {
		predicate := "EXISTS"
		if !*input.HasAnalysis {
			predicate = "NOT EXISTS"
		}
		conditions = append(conditions, predicate+" (SELECT 1 FROM call_analyses ca WHERE ca.call_uuid=c.call_uuid AND ca.status='done')")
	}
	if input.HasActions != nil {
		predicate := "EXISTS"
		if !*input.HasActions {
			predicate = "NOT EXISTS"
		}
		conditions = append(conditions, predicate+" (SELECT 1 FROM call_actions aa WHERE aa.call_uuid=c.call_uuid)")
	}
	if input.HasProcessingError != nil {
		if *input.HasProcessingError {
			conditions = append(conditions, "(c.status='failed' OR ii.error_code IS NOT NULL)")
		} else {
			conditions = append(conditions, "(c.status<>'failed' AND ii.error_code IS NULL)")
		}
	}
	if input.FavoriteOnly {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM user_favorite_calls ufc WHERE ufc.call_uuid=c.call_uuid AND ufc.user_uuid=$1)")
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
	if len(input.FolderUUIDs) > 0 {
		args = append(args, input.FolderUUIDs)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM call_folder_assignments cfa
			JOIN call_folders cf ON cf.folder_uuid=cfa.folder_uuid
			WHERE cfa.call_uuid=c.call_uuid AND cf.folder_uuid=ANY($%d) AND cf.deleted_at IS NULL
		)`, len(args)))
	}
	if input.Cursor != nil {
		expression := callSortExpression(input.Sort)
		operator := "<"
		if callSortOrder(input.Order) == "ASC" {
			operator = ">"
		}
		if input.Sort == "duration" && input.Cursor.DurationValue != nil {
			args = append(args, *input.Cursor.DurationValue, input.Cursor.CallID)
			conditions = append(conditions, fmt.Sprintf("(%s,c.call_uuid) %s ($%d,$%d)", expression, operator, len(args)-1, len(args)))
		} else if input.Cursor.TimeValue != nil {
			args = append(args, *input.Cursor.TimeValue, input.Cursor.CallID)
			conditions = append(conditions, fmt.Sprintf("(%s,c.call_uuid) %s ($%d,$%d)", expression, operator, len(args)-1, len(args)))
		}
	}

	return strings.Join(conditions, " AND "), args
}

func callSortExpression(sort string) string {
	switch sort {
	case "created_at":
		return "c.created_at"
	case "duration":
		return "c.duration_seconds"
	default:
		return "COALESCE(c.occurred_at,c.created_at)"
	}
}

func callSortOrder(order string) string {
	if strings.EqualFold(order, "asc") {
		return "ASC"
	}
	return "DESC"
}

func cursorForCall(call model.Call, sort, order string) *model.CallListCursor {
	cursor := &model.CallListCursor{CallID: call.ID, Sort: sort, Order: strings.ToLower(order)}
	if sort == "duration" {
		value := call.DurationSeconds
		cursor.SortValue = strconv.Itoa(value)
		cursor.DurationValue = &value
		return cursor
	}
	value := call.DisplayTime
	if sort == "created_at" {
		value = call.CreatedAt
	}
	cursor.SortValue = value.Format(time.RFC3339Nano)
	cursor.TimeValue = &value
	return cursor
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
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
