package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) ActivatePersonalSubscription(ctx context.Context, input models.ActivatePersonalSubscriptionInput) (models.Subscription, error) {
	if input.UserUUID == uuid.Nil {
		return models.Subscription{}, models.ErrInvalidBillingInput
	}

	if input.PlanCode == "" {
		input.PlanCode = models.PlanCodePersonalStart
	}

	plan, err := s.repository.GetPlanByCode(ctx, input.PlanCode)
	if err != nil {
		return models.Subscription{}, err
	}
	if plan.Type != models.PlanTypePersonal {
		return models.Subscription{}, models.ErrInvalidBillingInput
	}

	subscription, err := s.repository.ActivatePersonalSubscription(ctx, input, s.now())
	if err != nil {
		return models.Subscription{}, err
	}

	return s.applyManagerPersonalBenefit(ctx, input.UserUUID, subscription)
}
