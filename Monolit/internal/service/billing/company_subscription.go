package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) ActivateCompanySubscription(ctx context.Context, input models.ActivateCompanySubscriptionInput) (models.Subscription, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.Subscription{}, models.ErrInvalidBillingInput
	}

	if input.PlanCode == "" {
		input.PlanCode = models.PlanCodeBusinessStart
	}

	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.Subscription{}, err
	}

	plan, err := s.repository.GetPlanByCode(ctx, input.PlanCode)
	if err != nil {
		return models.Subscription{}, err
	}
	if plan.Type != models.PlanTypeBusiness {
		return models.Subscription{}, models.ErrInvalidBillingInput
	}

	return s.repository.ActivateCompanySubscription(ctx, input, s.now())
}

func (s *Service) CancelCompanySubscription(ctx context.Context, input models.CancelCompanySubscriptionInput) (models.Subscription, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.Subscription{}, models.ErrInvalidBillingInput
	}

	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.Subscription{}, err
	}

	return s.repository.CancelCompanySubscription(ctx, input.CompanyUUID, s.now())
}
