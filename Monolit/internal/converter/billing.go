package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

func PlanModelToAPI(plan models.Plan) (dto.PlanResponse, error) {
	return dto.PlanResponse{
		ID:                             plan.ID.String(),
		Code:                           string(plan.Code),
		Type:                           string(plan.Type),
		Name:                           plan.Name,
		MonthlyMinutesLimit:            plan.MonthlyMinutesLimit,
		ActiveInstructionLimit:         plan.ActiveInstructionLimit,
		CompanyLimit:                   plan.CompanyLimit,
		DepartmentsPerCompanyLimit:     plan.DepartmentsPerCompanyLimit,
		MembersPerCompanyLimit:         plan.MembersPerCompanyLimit,
		InstructionsPerDepartmentLimit: plan.InstructionsPerDepartmentLimit,
		AnalysisLevel:                  string(plan.AnalysisLevel),
		HistoryRetentionDays:           plan.HistoryRetentionDays,
		ExportEnabled:                  plan.ExportEnabled,
		TeamAnalyticsEnabled:           plan.TeamAnalyticsEnabled,
		APIAccessEnabled:               plan.APIAccessEnabled,
	}, nil
}

func SubscriptionModelToAPI(subscription models.Subscription) (dto.SubscriptionResponse, error) {
	plan, err := PlanModelToAPI(subscription.Plan)
	if err != nil {
		return dto.SubscriptionResponse{}, err
	}

	var userUUID *string
	if subscription.UserUUID.Valid {
		value := subscription.UserUUID.UUID.String()
		userUUID = &value
	}

	var companyUUID *string
	if subscription.CompanyUUID.Valid {
		value := subscription.CompanyUUID.UUID.String()
		companyUUID = &value
	}

	var endsAt *string
	if subscription.EndsAt != nil {
		value := formatBillingTime(*subscription.EndsAt)
		endsAt = &value
	}

	return dto.SubscriptionResponse{
		ID:          subscription.ID.String(),
		Plan:        plan,
		UserUUID:    userUUID,
		CompanyUUID: companyUUID,
		Status:      string(subscription.Status),
		StartsAt:    formatBillingTime(subscription.StartsAt),
		EndsAt:      endsAt,
		CreatedAt:   formatBillingTime(subscription.CreatedAt),
		UpdatedAt:   formatBillingTime(subscription.UpdatedAt),
	}, nil
}

func SubscriptionUsageModelToAPI(usage models.SubscriptionUsage) (dto.SubscriptionUsageResponse, error) {
	subscription, err := SubscriptionModelToAPI(usage.Subscription)
	if err != nil {
		return dto.SubscriptionUsageResponse{}, err
	}

	return dto.SubscriptionUsageResponse{
		Subscription:            subscription,
		PeriodStart:             formatBillingTime(usage.PeriodStart),
		PeriodEnd:               formatBillingTime(usage.PeriodEnd),
		UsedMinutes:             usage.UsedMinutes,
		LimitMinutes:            usage.LimitMinutes,
		RemainingMinutes:        usage.RemainingMinutes,
		Percent:                 usage.Percent,
		MembersLimit:            usage.MembersLimit,
		MembersUsed:             usage.MembersUsed,
		DepartmentsLimit:        usage.DepartmentsLimit,
		DepartmentsUsed:         usage.DepartmentsUsed,
		ActiveInstructionsLimit: usage.ActiveInstructionsLimit,
		ActiveInstructionsUsed:  usage.ActiveInstructionsUsed,
	}, nil
}

func formatBillingTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
