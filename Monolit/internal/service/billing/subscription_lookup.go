package billing

import (
	"context"
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) activePersonalSubscription(ctx context.Context, userID uuid.UUID) (models.Subscription, error) {
	subscription, err := s.repository.GetActivePersonalSubscription(ctx, userID)
	subscription, err = normalizeSubscriptionError(subscription, err)
	if err != nil {
		return models.Subscription{}, err
	}

	return s.applyManagerPersonalBenefit(ctx, userID, subscription)
}

func (s *Service) activeBusinessSubscription(ctx context.Context, companyID uuid.UUID) (models.Subscription, error) {
	subscription, err := s.repository.GetActiveBusinessSubscription(ctx, companyID)
	return normalizeSubscriptionError(subscription, err)
}

func (s *Service) applyManagerPersonalBenefit(ctx context.Context, userID uuid.UUID, subscription models.Subscription) (models.Subscription, error) {
	businessSubscription, err := s.repository.GetBestActiveBusinessSubscriptionForManager(ctx, userID)
	if err != nil {
		if errors.Is(err, models.ErrSubscriptionNotFound) {
			return subscription, nil
		}
		return models.Subscription{}, err
	}

	benefitCode := managerPersonalBenefitPlanCode(businessSubscription.Plan.Code)
	if benefitCode == "" || personalPlanRank(subscription.Plan.Code) >= personalPlanRank(benefitCode) {
		return subscription, nil
	}

	benefitPlan, err := s.repository.GetPlanByCode(ctx, benefitCode)
	if err != nil {
		return models.Subscription{}, err
	}

	subscription.Plan = benefitPlan
	return subscription, nil
}

func managerPersonalBenefitPlanCode(code models.PlanCode) models.PlanCode {
	switch code {
	case models.PlanCodeBusinessStart, models.PlanCodeBusinessPlus:
		return models.PlanCodePersonalPlus
	case models.PlanCodeBusinessPro:
		return models.PlanCodePersonalPro
	default:
		return ""
	}
}

func personalPlanRank(code models.PlanCode) int {
	switch code {
	case models.PlanCodePersonalStart:
		return 1
	case models.PlanCodePersonalPlus:
		return 2
	case models.PlanCodePersonalPro:
		return 3
	default:
		return 0
	}
}

func normalizeSubscriptionError(subscription models.Subscription, err error) (models.Subscription, error) {
	if err == nil {
		return subscription, nil
	}

	if errors.Is(err, models.ErrSubscriptionNotFound) {
		return models.Subscription{}, models.ErrSubscriptionRequired
	}

	return models.Subscription{}, err
}
