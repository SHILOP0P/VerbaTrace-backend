package dto

type ActivateSubscriptionRequest struct {
	PlanCode string `json:"plan_code"`
}

type PlanResponse struct {
	ID                             string `json:"id"`
	Code                           string `json:"code"`
	Type                           string `json:"type"`
	Name                           string `json:"name"`
	MonthlyPriceMinor              int64  `json:"monthly_price_minor"`
	Currency                       string `json:"currency"`
	MarketingHoursHint             int    `json:"marketing_hours_hint"`
	MonthlyMinutesLimit            int    `json:"monthly_minutes_limit"`
	MonthlyCreditAllowance         int64  `json:"monthly_credit_allowance"`
	ActiveInstructionLimit         int    `json:"active_instruction_limit"`
	CompanyLimit                   *int   `json:"company_limit"`
	DepartmentsPerCompanyLimit     *int   `json:"departments_per_company_limit"`
	MembersPerCompanyLimit         *int   `json:"members_per_company_limit"`
	InstructionsPerDepartmentLimit *int   `json:"instructions_per_department_limit"`
	AnalysisLevel                  string `json:"analysis_level"`
	HistoryRetentionDays           int    `json:"history_retention_days"`
	ExportEnabled                  bool   `json:"export_enabled"`
	TeamAnalyticsEnabled           bool   `json:"team_analytics_enabled"`
	APIAccessEnabled               bool   `json:"api_access_enabled"`
	WebhooksEnabled                bool   `json:"webhooks_enabled"`
}

type PlansResponse struct {
	Plans []PlanResponse `json:"plans"`
}

type SubscriptionResponse struct {
	ID          string       `json:"id"`
	Plan        PlanResponse `json:"plan"`
	UserUUID    *string      `json:"user_uuid"`
	CompanyUUID *string      `json:"company_uuid"`
	Status      string       `json:"status"`
	StartsAt    string       `json:"starts_at"`
	EndsAt      *string      `json:"ends_at"`
	CreatedAt   string       `json:"created_at"`
	UpdatedAt   string       `json:"updated_at"`
}

type SubscriptionUsageResponse struct {
	Subscription            SubscriptionResponse `json:"subscription"`
	PeriodStart             string               `json:"period_start"`
	PeriodEnd               string               `json:"period_end"`
	UsedMinutes             int                  `json:"used_minutes"`
	LimitMinutes            int                  `json:"limit_minutes"`
	RemainingMinutes        int                  `json:"remaining_minutes"`
	Percent                 float64              `json:"percent"`
	RemainingPercent        float64              `json:"remaining_percent"`
	DaysUntilReset          int                  `json:"days_until_reset"`
	ResetsAt                string               `json:"resets_at"`
	AllowanceExhausted      bool                 `json:"allowance_exhausted"`
	WalletCredits           *int64               `json:"wallet_credits,omitempty"`
	MembersLimit            *int                 `json:"members_limit,omitempty"`
	MembersUsed             *int                 `json:"members_used,omitempty"`
	DepartmentsLimit        *int                 `json:"departments_limit,omitempty"`
	DepartmentsUsed         *int                 `json:"departments_used,omitempty"`
	ActiveInstructionsLimit *int                 `json:"active_instructions_limit,omitempty"`
	ActiveInstructionsUsed  *int                 `json:"active_instructions_used,omitempty"`
}
