package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) CanAccessAPI(ctx context.Context, companyID uuid.UUID) error {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return err
	}

	if !subscription.Plan.APIAccessEnabled {
		return models.ErrAPIAccessDenied
	}

	return nil
}

func (s *Service) CanExportReports(ctx context.Context, companyID uuid.UUID) error {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return err
	}

	if !subscription.Plan.ExportEnabled {
		return models.ErrExportAccessDenied
	}

	return nil
}

func (s *Service) CanAccessTeamAnalytics(ctx context.Context, companyID uuid.UUID) error {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return err
	}

	if !subscription.Plan.TeamAnalyticsEnabled {
		return models.ErrTeamAnalyticsAccessDenied
	}

	return nil
}
