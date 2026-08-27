package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) CanUseCompany(ctx context.Context, companyID uuid.UUID) error {
	if companyID == uuid.Nil {
		return models.ErrInvalidBillingInput
	}

	_, err := s.activeBusinessSubscription(ctx, companyID)
	return err
}

func (s *Service) CanCreateCompany(ctx context.Context, ownerID uuid.UUID) error {
	return nil
}

func (s *Service) CanCreateDepartment(ctx context.Context, companyID uuid.UUID) error {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return err
	}

	if subscription.Plan.DepartmentsPerCompanyLimit == nil {
		return models.ErrDepartmentLimitExceeded
	}

	count, err := s.repository.CountCompanyDepartments(ctx, companyID)
	if err != nil {
		return err
	}

	if count >= *subscription.Plan.DepartmentsPerCompanyLimit {
		return models.ErrDepartmentLimitExceeded
	}

	return nil
}

func (s *Service) CanAddCompanyMember(ctx context.Context, companyID uuid.UUID) error {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return err
	}

	if subscription.Plan.MembersPerCompanyLimit == nil {
		return nil
	}

	count, err := s.repository.CountCompanyMembers(ctx, companyID)
	if err != nil {
		return err
	}

	if count >= *subscription.Plan.MembersPerCompanyLimit {
		return models.ErrMemberLimitExceeded
	}

	return nil
}
