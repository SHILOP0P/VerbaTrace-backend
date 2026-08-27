package billing

import (
	"context"
	"math"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) GetPersonalSubscriptionUsage(ctx context.Context, input models.GetPersonalSubscriptionUsageInput) (models.SubscriptionUsage, error) {
	if input.UserUUID == uuid.Nil {
		return models.SubscriptionUsage{}, models.ErrInvalidBillingInput
	}

	subscription, err := s.GetPersonalSubscription(ctx, input.UserUUID)
	if err != nil {
		return models.SubscriptionUsage{}, err
	}

	return s.subscriptionUsage(ctx, subscription, usagePeriodStart(input.PeriodStart, s.now()))
}

func (s *Service) GetCompanySubscriptionUsage(ctx context.Context, input models.GetCompanySubscriptionUsageInput) (models.SubscriptionUsage, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.SubscriptionUsage{}, models.ErrInvalidBillingInput
	}

	subscription, err := s.GetCompanySubscription(ctx, models.GetCompanySubscriptionInput{
		CompanyUUID: input.CompanyUUID,
		RequestUser: input.RequestUser,
	})
	if err != nil {
		return models.SubscriptionUsage{}, err
	}

	usage, err := s.subscriptionUsage(ctx, subscription, usagePeriodStart(input.PeriodStart, s.now()))
	if err != nil {
		return models.SubscriptionUsage{}, err
	}

	if subscription.Plan.MembersPerCompanyLimit != nil {
		used, err := s.repository.CountCompanyMembers(ctx, input.CompanyUUID)
		if err != nil {
			return models.SubscriptionUsage{}, err
		}
		usage.MembersLimit = subscription.Plan.MembersPerCompanyLimit
		usage.MembersUsed = &used
	}

	if subscription.Plan.DepartmentsPerCompanyLimit != nil {
		used, err := s.repository.CountCompanyDepartments(ctx, input.CompanyUUID)
		if err != nil {
			return models.SubscriptionUsage{}, err
		}
		usage.DepartmentsLimit = subscription.Plan.DepartmentsPerCompanyLimit
		usage.DepartmentsUsed = &used
	}

	if subscription.Plan.InstructionsPerDepartmentLimit != nil {
		used, err := s.repository.CountActiveInstructions(ctx, models.ListAnalysisInstructionsInput{
			Scope:       models.AnalysisInstructionScopeCompany,
			CompanyUUID: uuid.NullUUID{UUID: input.CompanyUUID, Valid: true},
		})
		if err != nil {
			return models.SubscriptionUsage{}, err
		}
		usage.ActiveInstructionsLimit = subscription.Plan.InstructionsPerDepartmentLimit
		usage.ActiveInstructionsUsed = &used
	}

	return usage, nil
}

func (s *Service) subscriptionUsage(ctx context.Context, subscription models.Subscription, periodStart time.Time) (models.SubscriptionUsage, error) {
	usedMinutes, err := s.repository.CountUsedMinutes(ctx, subscription.ID, periodStart)
	if err != nil {
		return models.SubscriptionUsage{}, err
	}

	limitMinutes := subscription.Plan.MonthlyMinutesLimit
	remainingMinutes := limitMinutes - usedMinutes
	if remainingMinutes < 0 {
		remainingMinutes = 0
	}

	periodEnd := periodStart.AddDate(0, 1, 0)
	usedCredits := int64(usedMinutes) * 875
	remainingCredits := subscription.Plan.MonthlyCreditAllowance - usedCredits
	var walletCredits *int64
	if s.creditRepository != nil {
		creditUsage, creditErr := s.creditRepository.EnsureCurrentCreditUsage(ctx, subscription, s.now())
		if creditErr != nil {
			return models.SubscriptionUsage{}, creditErr
		}
		remainingCredits = creditUsage.AllowanceRemaining
		periodEnd = creditUsage.ResetsAt
		if remainingCredits == 0 && creditUsage.WalletCredits > 0 {
			wallet := creditUsage.WalletCredits
			walletCredits = &wallet
		}
	}
	if remainingCredits < 0 {
		remainingCredits = 0
	}
	remainingPercent := remainingUsagePercent(remainingCredits, subscription.Plan.MonthlyCreditAllowance)

	return models.SubscriptionUsage{
		Subscription:       subscription,
		PeriodStart:        periodStart,
		PeriodEnd:          periodEnd,
		UsedMinutes:        usedMinutes,
		LimitMinutes:       limitMinutes,
		RemainingMinutes:   remainingMinutes,
		Percent:            usagePercent(usedMinutes, limitMinutes),
		RemainingPercent:   remainingPercent,
		DaysUntilReset:     daysUntilReset(s.now(), periodEnd),
		ResetsAt:           periodEnd,
		AllowanceExhausted: remainingCredits == 0,
		WalletCredits:      walletCredits,
	}, nil
}

func remainingUsagePercent(remaining, allowance int64) float64 {
	if allowance <= 0 {
		return 0
	}
	percent := float64(remaining) / float64(allowance) * 100
	return math.Round(percent*100) / 100
}

func daysUntilReset(now, resetsAt time.Time) int {
	remaining := resetsAt.Sub(now.UTC())
	if remaining <= 0 {
		return 0
	}
	return int(math.Ceil(remaining.Hours() / 24))
}

func usagePeriodStart(periodStart *time.Time, now time.Time) time.Time {
	if periodStart != nil {
		return usageMonthStart(*periodStart)
	}

	return usageMonthStart(now)
}

func usageMonthStart(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func usagePercent(usedMinutes int, limitMinutes int) float64 {
	if limitMinutes <= 0 {
		return 0
	}

	percent := float64(usedMinutes) / float64(limitMinutes) * 100
	return math.Round(percent*100) / 100
}
