package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

const planColumns = `
	plan_uuid,
	code,
	type,
	name,
	monthly_price_minor,
	currency,
	marketing_hours_hint,
	monthly_minutes_limit,
	monthly_credit_allowance,
	active_instruction_limit,
	company_limit,
	departments_per_company_limit,
	members_per_company_limit,
	instructions_per_department_limit,
	analysis_level,
	history_retention_days,
	export_enabled,
	team_analytics_enabled,
	api_access_enabled,
	webhooks_enabled,
	created_at,
	updated_at
`

func (r *Repository) GetPlanByCode(ctx context.Context, code models.PlanCode) (models.Plan, error) {
	query := `
	SELECT ` + planColumns + `
	FROM plans
	WHERE code = $1
	`

	plan, err := scanPlan(r.db.QueryRowContext(ctx, query, code))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Plan{}, models.ErrPlanNotFound
		}
		return models.Plan{}, fmt.Errorf("get plan by code: %w", err)
	}

	return plan, nil
}

func (r *Repository) ListPlans(ctx context.Context) ([]models.Plan, error) {
	query := `
	SELECT ` + planColumns + `
	FROM plans
	ORDER BY CASE code
	    WHEN 'personal_start' THEN 1
	    WHEN 'personal_plus' THEN 2
	    WHEN 'personal_pro' THEN 3
	    WHEN 'business_start' THEN 4
	    WHEN 'business_plus' THEN 5
	    WHEN 'business_pro' THEN 6
	    ELSE 100
	END
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer func() { _ = rows.Close() }()

	plans := make([]models.Plan, 0)
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("list plans: %w", err)
		}
		plans = append(plans, plan)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}

	return plans, nil
}

type planScanner interface {
	Scan(dest ...any) error
}

func scanPlan(row planScanner) (models.Plan, error) {
	var plan models.Plan
	var code string
	var planType string
	var companyLimit sql.NullInt64
	var departmentsPerCompanyLimit sql.NullInt64
	var membersPerCompanyLimit sql.NullInt64
	var instructionsPerDepartmentLimit sql.NullInt64
	var analysisLevel string

	if err := row.Scan(
		&plan.ID,
		&code,
		&planType,
		&plan.Name,
		&plan.MonthlyPriceMinor,
		&plan.Currency,
		&plan.MarketingHoursHint,
		&plan.MonthlyMinutesLimit,
		&plan.MonthlyCreditAllowance,
		&plan.ActiveInstructionLimit,
		&companyLimit,
		&departmentsPerCompanyLimit,
		&membersPerCompanyLimit,
		&instructionsPerDepartmentLimit,
		&analysisLevel,
		&plan.HistoryRetentionDays,
		&plan.ExportEnabled,
		&plan.TeamAnalyticsEnabled,
		&plan.APIAccessEnabled,
		&plan.WebhooksEnabled,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	); err != nil {
		return models.Plan{}, err
	}

	plan.Code = models.PlanCode(code)
	plan.Type = models.PlanType(planType)
	plan.CompanyLimit = nullableInt(companyLimit)
	plan.DepartmentsPerCompanyLimit = nullableInt(departmentsPerCompanyLimit)
	plan.MembersPerCompanyLimit = nullableInt(membersPerCompanyLimit)
	plan.InstructionsPerDepartmentLimit = nullableInt(instructionsPerDepartmentLimit)
	plan.AnalysisLevel = models.AnalysisLevel(analysisLevel)

	return plan, nil
}

func nullableInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}

	intValue := int(value.Int64)
	return &intValue
}

func nullableUUID(value uuid.NullUUID) any {
	if !value.Valid {
		return nil
	}

	return value.UUID
}
