package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) AnalysisLevelForUser(ctx context.Context, userID uuid.UUID) (models.AnalysisLevel, error) {
	subscription, err := s.activePersonalSubscription(ctx, userID)
	if err != nil {
		return "", err
	}

	return subscription.Plan.AnalysisLevel, nil
}

func (s *Service) AnalysisLevelForCompany(ctx context.Context, companyID uuid.UUID) (models.AnalysisLevel, error) {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return "", err
	}

	return subscription.Plan.AnalysisLevel, nil
}
