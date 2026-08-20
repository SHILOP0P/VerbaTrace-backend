package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) GetPersonalSubscription(ctx context.Context, userID uuid.UUID) (models.Subscription, error) {
	if userID == uuid.Nil {
		return models.Subscription{}, models.ErrInvalidBillingInput
	}

	subscription, err := s.repository.GetActivePersonalSubscription(ctx, userID)
	if err != nil {
		return models.Subscription{}, err
	}

	return s.applyManagerPersonalBenefit(ctx, userID, subscription)
}

func (s *Service) GetCompanySubscription(ctx context.Context, input models.GetCompanySubscriptionInput) (models.Subscription, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.Subscription{}, models.ErrInvalidBillingInput
	}

	if err := s.requireActiveCompanyMember(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.Subscription{}, err
	}

	return s.repository.GetActiveBusinessSubscription(ctx, input.CompanyUUID)
}
