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

// CanExportReports and CanAccessTeamAnalytics are reading permissions, and
// reading in a frozen company works exactly as it does in an active one. They
// therefore ask which plan the company is on rather than whether it may still
// spend, which is what used to refuse both in a frozen company.
func (s *Service) CanExportReports(ctx context.Context, companyID uuid.UUID) error {
	subscription, err := s.companyPlan(ctx, companyID)
	if err != nil {
		return err
	}

	if !subscription.Plan.ExportEnabled {
		return models.ErrExportAccessDenied
	}

	return nil
}

func (s *Service) CanAccessTeamAnalytics(ctx context.Context, companyID uuid.UUID) error {
	subscription, err := s.companyPlan(ctx, companyID)
	if err != nil {
		return err
	}

	if !subscription.Plan.TeamAnalyticsEnabled {
		return models.ErrTeamAnalyticsAccessDenied
	}

	return nil
}

// companyPlan reads the plan regardless of the company's lifecycle state, and
// falls back to the active-subscription lookup when the repository is older than
// this distinction.
func (s *Service) companyPlan(ctx context.Context, companyID uuid.UUID) (models.Subscription, error) {
	reader, ok := s.repository.(interface {
		GetPlanForCompany(ctx context.Context, companyID uuid.UUID) (models.Subscription, error)
	})
	if !ok {
		return s.activeBusinessSubscription(ctx, companyID)
	}

	return reader.GetPlanForCompany(ctx, companyID)
}
