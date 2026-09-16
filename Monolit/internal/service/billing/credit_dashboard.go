package billing

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type creditDashboardRepository interface {
	GetCreditDashboard(context.Context, models.Subscription, time.Time, time.Time) (models.CreditDashboard, error)
}

type companyCreditVisibilityRepository interface {
	GetCompanyCreditUsageVisibility(context.Context, uuid.UUID) (bool, error)
	UpdateCompanyCreditUsageVisibility(context.Context, uuid.UUID, bool) error
}

func (s *Service) GetPersonalCreditDashboard(ctx context.Context, userID uuid.UUID, from, to time.Time) (models.CreditDashboard, error) {
	if userID == uuid.Nil || !validActivityRange(from, to) {
		return models.CreditDashboard{}, models.ErrInvalidBillingInput
	}
	subscription, err := s.GetPersonalSubscription(ctx, userID)
	if err != nil {
		return models.CreditDashboard{}, err
	}
	return s.creditDashboard(ctx, subscription, from, to)
}

func (s *Service) GetCompanyCreditDashboard(ctx context.Context, companyID, userID uuid.UUID, from, to time.Time) (models.CreditDashboard, error) {
	if companyID == uuid.Nil || userID == uuid.Nil || !validActivityRange(from, to) {
		return models.CreditDashboard{}, models.ErrInvalidBillingInput
	}
	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return models.CreditDashboard{}, err
	}
	visibilityRepository, ok := s.companyRepository.(companyCreditVisibilityRepository)
	if !ok {
		return models.CreditDashboard{}, models.ErrInvalidBillingInput
	}
	visibleToMembers, err := visibilityRepository.GetCompanyCreditUsageVisibility(ctx, companyID)
	if err != nil {
		return models.CreditDashboard{}, err
	}
	canManage := member.Role.ManagesCompany()
	if !canManage && !visibleToMembers {
		return models.CreditDashboard{}, models.ErrForbidden
	}
	subscription, err := s.repository.GetActiveBusinessSubscription(ctx, companyID)
	if err != nil {
		return models.CreditDashboard{}, err
	}
	dashboard, err := s.creditDashboard(ctx, subscription, from, to)
	if err != nil {
		return models.CreditDashboard{}, err
	}
	dashboard.VisibleToMembers = visibleToMembers
	dashboard.CanManageVisibility = canManage
	return dashboard, nil
}

func (s *Service) UpdateCompanyCreditVisibility(ctx context.Context, input models.UpdateCompanyCreditVisibilityInput) (models.CreditDashboard, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.CreditDashboard{}, models.ErrInvalidBillingInput
	}
	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.CreditDashboard{}, err
	}
	repository, ok := s.companyRepository.(companyCreditVisibilityRepository)
	if !ok {
		return models.CreditDashboard{}, models.ErrInvalidBillingInput
	}
	if err := repository.UpdateCompanyCreditUsageVisibility(ctx, input.CompanyUUID, input.Visible); err != nil {
		return models.CreditDashboard{}, err
	}
	return models.CreditDashboard{VisibleToMembers: input.Visible, CanManageVisibility: true}, nil
}

func (s *Service) creditDashboard(ctx context.Context, subscription models.Subscription, from, to time.Time) (models.CreditDashboard, error) {
	if s.creditDashboardRepo == nil {
		return models.CreditDashboard{}, models.ErrInvalidBillingInput
	}
	return s.creditDashboardRepo.GetCreditDashboard(ctx, subscription, from, to)
}

func validActivityRange(from, to time.Time) bool {
	return !from.IsZero() && to.After(from) && to.Sub(from) <= 370*24*time.Hour
}
