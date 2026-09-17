package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// CancelCompanySubscription drops the business plan that covers this company.
// Buying one is an administrator's job for now, but stopping one is the owner's
// own decision, and only theirs: the deputy never touches the subscription.
func (s *Service) CancelCompanySubscription(ctx context.Context, input models.CancelCompanySubscriptionInput) (models.Subscription, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.Subscription{}, models.ErrInvalidBillingInput
	}

	if err := s.requireCompanyOwner(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.Subscription{}, err
	}

	return s.repository.CancelCompanySubscription(ctx, input.CompanyUUID, s.now())
}
