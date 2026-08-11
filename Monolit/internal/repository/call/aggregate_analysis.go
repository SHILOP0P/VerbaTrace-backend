package call

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const aggregateAnalysisColumns = `
	aggregate_analysis_uuid,
	scope,
	user_uuid,
	company_uuid,
	department_uuid,
	folder_uuid,
	period_from,
	period_to,
	status,
	provider,
	model,
	source_calls_count,
	result_json,
	result_text,
	error_message,
	created_by_user_uuid,
	created_at,
	updated_at
`

func (r *Repository) CreateAggregateAnalysis(ctx context.Context, analysis models.AggregateAnalysis) (models.AggregateAnalysis, error) {
	query := `INSERT INTO aggregate_analyses (
		aggregate_analysis_uuid, scope, user_uuid, company_uuid, department_uuid, folder_uuid,
		period_from, period_to, status, provider, model, source_calls_count, result_json,
		result_text, error_message, created_by_user_uuid, created_at, updated_at
	) VALUES (
		$1, $2, $3, $4, $5, $6,
		$7, $8, $9, $10, $11, $12, $13::jsonb,
		$14, $15, $16, $17, $18
	) RETURNING ` + aggregateAnalysisColumns

	row := r.db.QueryRowContext(ctx, query,
		analysis.ID, analysis.Scope, nullUUIDArg(analysis.UserUUID), nullUUIDArg(analysis.CompanyUUID),
		nullUUIDArg(analysis.DepartmentUUID), nullUUIDArg(analysis.FolderUUID), analysis.PeriodFrom,
		analysis.PeriodTo, analysis.Status, analysis.Provider, analysis.Model, analysis.SourceCallsCount,
		jsonArg(analysis.ResultJSON), analysis.ResultText, analysis.ErrorMessage, analysis.CreatedByUserUUID,
		analysis.CreatedAt, analysis.UpdatedAt,
	)
	return scanAggregateAnalysis(row, "create aggregate analysis")
}

func (r *Repository) GetAggregateAnalysisByUUID(ctx context.Context, id uuid.UUID) (models.AggregateAnalysis, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+aggregateAnalysisColumns+` FROM aggregate_analyses WHERE aggregate_analysis_uuid = $1`, id)
	return scanAggregateAnalysis(row, "get aggregate analysis")
}

func (r *Repository) FindReusableAggregateAnalysis(ctx context.Context, input models.CreateDeepAnalysisInput) (models.AggregateAnalysis, error) {
	where := []string{"scope = $1", "period_from = $2", "period_to = $3", "status <> 'failed'"}
	args := []any{input.Scope, input.PeriodFrom, input.PeriodTo}
	where = appendSubjectWhere(where, &args, input.Scope, input.CompanyUUID, input.DepartmentUUID, input.FolderUUID, uuid.NullUUID{UUID: input.UserID, Valid: input.Scope == models.AggregateAnalysisScopePersonal})
	query := `SELECT ` + aggregateAnalysisColumns + ` FROM aggregate_analyses WHERE ` + strings.Join(where, " AND ") + ` ORDER BY created_at DESC LIMIT 1`
	row := r.db.QueryRowContext(ctx, query, args...)
	return scanAggregateAnalysis(row, "find reusable aggregate analysis")
}

func (r *Repository) ListAggregateAnalyses(ctx context.Context, input models.ListDeepAnalysesInput) (models.ListAggregateAnalysesResult, error) {
	where, args := visibleAggregateAnalysisWhere(input)
	query := fmt.Sprintf(`SELECT %s FROM aggregate_analyses aa WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		aggregateAnalysisColumns, strings.Join(where, " AND "), len(args)+1, len(args)+2)
	rows, err := r.db.QueryContext(ctx, query, append(args, input.Limit, input.Offset)...)
	if err != nil {
		return models.ListAggregateAnalysesResult{}, fmt.Errorf("list aggregate analyses: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []models.AggregateAnalysis{}
	for rows.Next() {
		item, err := scanAggregateAnalysis(rows, "scan aggregate analysis")
		if err != nil {
			return models.ListAggregateAnalysesResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return models.ListAggregateAnalysesResult{}, fmt.Errorf("list aggregate analyses: %w", err)
	}
	countQuery := `SELECT COUNT(*)::int FROM aggregate_analyses aa WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return models.ListAggregateAnalysesResult{}, fmt.Errorf("count aggregate analyses: %w", err)
	}
	return models.ListAggregateAnalysesResult{Items: items, Total: total, Limit: input.Limit, Offset: input.Offset}, nil
}

func (r *Repository) MarkAggregateAnalysisProcessing(ctx context.Context, id uuid.UUID) (models.AggregateAnalysis, error) {
	row := r.db.QueryRowContext(ctx, `UPDATE aggregate_analyses SET status = 'processing', result_json = NULL, result_text = NULL, error_message = NULL, updated_at = now() WHERE aggregate_analysis_uuid = $1 RETURNING `+aggregateAnalysisColumns, id)
	return scanAggregateAnalysis(row, "mark aggregate analysis processing")
}

func (r *Repository) MarkAggregateAnalysisDone(ctx context.Context, id uuid.UUID, result models.AnalysisResult, sourceCallsCount int) (models.AggregateAnalysis, error) {
	row := r.db.QueryRowContext(ctx, `UPDATE aggregate_analyses SET status = 'done', model = $2, source_calls_count = $3, result_json = $4::jsonb, result_text = $5, error_message = NULL, updated_at = now() WHERE aggregate_analysis_uuid = $1 RETURNING `+aggregateAnalysisColumns,
		id, result.Model, sourceCallsCount, jsonArg(result.ResultJSON), result.ResultText)
	return scanAggregateAnalysis(row, "mark aggregate analysis done")
}

func (r *Repository) MarkAggregateAnalysisFailed(ctx context.Context, id uuid.UUID, errorMessage string) (models.AggregateAnalysis, error) {
	row := r.db.QueryRowContext(ctx, `UPDATE aggregate_analyses SET status = 'failed', result_json = NULL, result_text = NULL, error_message = $2, updated_at = now() WHERE aggregate_analysis_uuid = $1 RETURNING `+aggregateAnalysisColumns, id, errorMessage)
	return scanAggregateAnalysis(row, "mark aggregate analysis failed")
}

func (r *Repository) ListAggregateAnalysisSourceCalls(ctx context.Context, input models.AnalyticsOverviewInput) ([]models.AggregateAnalysisSourceCall, int, error) {
	where, args := buildListFilters(models.ListCallsInput{
		UserID: input.UserID, VisibilityScope: input.VisibilityScope, CompanyUUID: input.CompanyUUID,
		DepartmentUUID: input.DepartmentUUID, From: input.From, To: input.To, FolderUUID: input.FolderUUID,
	})
	countQuery := fmt.Sprintf(`SELECT COUNT(*)::int FROM calls c JOIN call_analyses ca ON ca.call_uuid = c.call_uuid AND ca.status = 'done' WHERE %s`, where)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count aggregate source calls: %w", err)
	}
	query := fmt.Sprintf(`SELECT c.call_uuid, c.created_at, c.title, effective_call_analysis_json(ca.analysis_uuid,ca.result_json) FROM calls c JOIN call_analyses ca ON ca.call_uuid = c.call_uuid AND ca.status = 'done' WHERE %s ORDER BY c.created_at DESC, c.call_uuid`, where)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list aggregate source calls: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []models.AggregateAnalysisSourceCall{}
	for rows.Next() {
		var id uuid.UUID
		var createdAt time.Time
		var title string
		var raw json.RawMessage
		if err := rows.Scan(&id, &createdAt, &title, &raw); err != nil {
			return nil, 0, fmt.Errorf("scan aggregate source call: %w", err)
		}
		items = append(items, compactSourceCall(id, createdAt, title, raw))
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list aggregate source calls: %w", err)
	}
	return items, total, nil
}

func (r *Repository) SpendDeepAnalysisUsage(ctx context.Context, subjectType models.DeepAnalysisSubjectType, subjectID uuid.UUID, periodStart time.Time, periodEnd time.Time) error {
	query := `
	INSERT INTO deep_analysis_usage_counters (
		counter_uuid, subject_type, subject_uuid, period_start, period_end, used_count, limit_count
	) VALUES ($1, $2, $3, $4, $5, 1, $6)
	ON CONFLICT (subject_type, subject_uuid, period_start) DO UPDATE
	SET used_count = deep_analysis_usage_counters.used_count + 1,
	    period_end = EXCLUDED.period_end,
	    updated_at = now()
	WHERE deep_analysis_usage_counters.used_count < deep_analysis_usage_counters.limit_count
	RETURNING used_count`
	var used int
	err := r.db.QueryRowContext(ctx, query, uuid.New(), subjectType, subjectID, periodStart, periodEnd, models.DeepAnalysisWeeklyLimit).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrDeepAnalysisLimitExceeded
	}
	if err != nil {
		return fmt.Errorf("spend deep analysis usage: %w", err)
	}
	return nil
}

func scanAggregateAnalysis(row interface{ Scan(dest ...any) error }, operation string) (models.AggregateAnalysis, error) {
	var item models.AggregateAnalysis
	var result sql.NullString
	var model sql.NullString
	var resultText sql.NullString
	var errorMessage sql.NullString
	err := row.Scan(
		&item.ID, &item.Scope, &item.UserUUID, &item.CompanyUUID, &item.DepartmentUUID, &item.FolderUUID,
		&item.PeriodFrom, &item.PeriodTo, &item.Status, &item.Provider, &model, &item.SourceCallsCount,
		&result, &resultText, &errorMessage, &item.CreatedByUserUUID, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.AggregateAnalysis{}, models.ErrAggregateAnalysisNotFound
		}
		return models.AggregateAnalysis{}, fmt.Errorf("%s: %w", operation, err)
	}
	if model.Valid {
		item.Model = &model.String
	}
	if result.Valid {
		item.ResultJSON = json.RawMessage(result.String)
	}
	if resultText.Valid {
		item.ResultText = &resultText.String
	}
	if errorMessage.Valid {
		item.ErrorMessage = &errorMessage.String
	}
	return item, nil
}

func appendSubjectWhere(where []string, args *[]any, scope models.AggregateAnalysisScope, companyID uuid.NullUUID, departmentID uuid.NullUUID, folderID uuid.NullUUID, userID uuid.NullUUID) []string {
	switch scope {
	case models.AggregateAnalysisScopePersonal:
		*args = append(*args, userID.UUID)
		where = append(where, fmt.Sprintf("user_uuid = $%d", len(*args)))
	case models.AggregateAnalysisScopeCompany:
		*args = append(*args, companyID.UUID)
		where = append(where, fmt.Sprintf("company_uuid = $%d", len(*args)))
	case models.AggregateAnalysisScopeDepartment:
		*args = append(*args, companyID.UUID, departmentID.UUID)
		where = append(where, fmt.Sprintf("company_uuid = $%d", len(*args)-1), fmt.Sprintf("department_uuid = $%d", len(*args)))
	case models.AggregateAnalysisScopeFolder:
		*args = append(*args, folderID.UUID)
		where = append(where, fmt.Sprintf("folder_uuid = $%d", len(*args)))
	}
	return where
}

func visibleAggregateAnalysisWhere(input models.ListDeepAnalysesInput) ([]string, []any) {
	where := []string{`(
		aa.user_uuid = $1
		OR aa.company_uuid IN (SELECT company_uuid FROM company_members WHERE user_uuid = $1 AND status = 'active')
		OR aa.department_uuid IN (SELECT department_uuid FROM department_members WHERE user_uuid = $1 AND status = 'active')
	)`}
	args := []any{input.UserID}
	if input.Scope != "" {
		args = append(args, input.Scope)
		where = append(where, fmt.Sprintf("aa.scope = $%d", len(args)))
	}
	if input.CompanyUUID.Valid {
		args = append(args, input.CompanyUUID.UUID)
		where = append(where, fmt.Sprintf("aa.company_uuid = $%d", len(args)))
	}
	if input.DepartmentUUID.Valid {
		args = append(args, input.DepartmentUUID.UUID)
		where = append(where, fmt.Sprintf("aa.department_uuid = $%d", len(args)))
	}
	if input.FolderUUID.Valid {
		args = append(args, input.FolderUUID.UUID)
		where = append(where, fmt.Sprintf("aa.folder_uuid = $%d", len(args)))
	}
	if input.From != nil {
		args = append(args, *input.From)
		where = append(where, fmt.Sprintf("aa.period_to >= $%d", len(args)))
	}
	if input.To != nil {
		args = append(args, *input.To)
		where = append(where, fmt.Sprintf("aa.period_from <= $%d", len(args)))
	}
	if input.Status != "" {
		args = append(args, input.Status)
		where = append(where, fmt.Sprintf("aa.status = $%d", len(args)))
	}
	return where, args
}

func compactSourceCall(id uuid.UUID, createdAt time.Time, title string, raw json.RawMessage) models.AggregateAnalysisSourceCall {
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	score, _ := numberValue(payload["score"])
	var scorePtr *float64
	if score > 0 {
		scorePtr = &score
	}
	summary, _ := payload["summary"].(string)
	return models.AggregateAnalysisSourceCall{
		CallUUID: id, CreatedAt: createdAt, Title: title, Score: scorePtr, Summary: strings.TrimSpace(summary),
		Topics: payload["topics"], CriteriaResults: payload["criteria_results"], BusinessOutcome: payload["business_outcome"],
		CustomerSignals: payload["customer_signals"], IssueCodes: payload["issue_codes"], Risks: payload["risks"],
		CustomerObjections: payload["customer_objections"], NextStepQuality: payload["next_step_quality"],
	}
}

func nullUUIDArg(value uuid.NullUUID) any {
	if value.Valid {
		return value.UUID
	}
	return nil
}

func jsonArg(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return []byte(value)
}
