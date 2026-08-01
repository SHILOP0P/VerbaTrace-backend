package billing

import (
	"database/sql"

	"verbatrace/monolit/internal/models"
)

func subscriptionColumns(subscriptionAlias string, planAlias string) string {
	return subscriptionAlias + `.subscription_uuid,
	       ` + subscriptionAlias + `.type,
	       ` + subscriptionAlias + `.user_uuid,
	       ` + subscriptionAlias + `.company_uuid,
	       ` + subscriptionAlias + `.status,
	       ` + subscriptionAlias + `.starts_at,
	       ` + subscriptionAlias + `.ends_at,
	       ` + subscriptionAlias + `.created_at,
	       ` + subscriptionAlias + `.updated_at,
	       ` + planAlias + `.plan_uuid,
	       ` + planAlias + `.code,
	       ` + planAlias + `.type,
	       ` + planAlias + `.name,
	       ` + planAlias + `.monthly_minutes_limit,
	       ` + planAlias + `.active_instruction_limit,
	       ` + planAlias + `.company_limit,
	       ` + planAlias + `.departments_per_company_limit,
	       ` + planAlias + `.members_per_company_limit,
	       ` + planAlias + `.instructions_per_department_limit,
	       ` + planAlias + `.analysis_level,
	       ` + planAlias + `.history_retention_days,
	       ` + planAlias + `.export_enabled,
	       ` + planAlias + `.team_analytics_enabled,
	       ` + planAlias + `.api_access_enabled,
	       ` + planAlias + `.created_at,
	       ` + planAlias + `.updated_at`
}

func scanSubscription(row planScanner) (models.Subscription, error) {
	var subscription models.Subscription
	var subscriptionType string
	var status string
	var endsAt sql.NullTime
	var planCode string
	var planType string
	var companyLimit sql.NullInt64
	var departmentsPerCompanyLimit sql.NullInt64
	var membersPerCompanyLimit sql.NullInt64
	var instructionsPerDepartmentLimit sql.NullInt64
	var analysisLevel string

	if err := row.Scan(
		&subscription.ID,
		&subscriptionType,
		&subscription.UserUUID,
		&subscription.CompanyUUID,
		&status,
		&subscription.StartsAt,
		&endsAt,
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
		&subscription.Plan.ID,
		&planCode,
		&planType,
		&subscription.Plan.Name,
		&subscription.Plan.MonthlyMinutesLimit,
		&subscription.Plan.ActiveInstructionLimit,
		&companyLimit,
		&departmentsPerCompanyLimit,
		&membersPerCompanyLimit,
		&instructionsPerDepartmentLimit,
		&analysisLevel,
		&subscription.Plan.HistoryRetentionDays,
		&subscription.Plan.ExportEnabled,
		&subscription.Plan.TeamAnalyticsEnabled,
		&subscription.Plan.APIAccessEnabled,
		&subscription.Plan.CreatedAt,
		&subscription.Plan.UpdatedAt,
	); err != nil {
		return models.Subscription{}, err
	}

	subscription.Status = models.SubscriptionStatus(status)
	subscription.Plan.Code = models.PlanCode(planCode)
	subscription.Plan.Type = models.PlanType(planType)
	subscription.Plan.CompanyLimit = nullableInt(companyLimit)
	subscription.Plan.DepartmentsPerCompanyLimit = nullableInt(departmentsPerCompanyLimit)
	subscription.Plan.MembersPerCompanyLimit = nullableInt(membersPerCompanyLimit)
	subscription.Plan.InstructionsPerDepartmentLimit = nullableInt(instructionsPerDepartmentLimit)
	subscription.Plan.AnalysisLevel = models.AnalysisLevel(analysisLevel)
	if endsAt.Valid {
		subscription.EndsAt = &endsAt.Time
	}

	return subscription, nil
}
