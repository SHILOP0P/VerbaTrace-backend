package billing

import (
	"context"
	"errors"

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

// CanCreateCompany answers whether the owner's plan covers one more company.
//
// Without a business plan the answer comes from the "free" plan, whose limits
// are explicit zeros. Reading a missing plan as an empty limit would mean "no
// cap" under the project's own rule, which is the opposite of the truth — and
// that is exactly how the limit ended up unenforced before.
func (s *Service) CanCreateCompany(ctx context.Context, ownerID uuid.UUID) error {
	if ownerID == uuid.Nil {
		return models.ErrInvalidBillingInput
	}

	limit, err := s.companyLimitFor(ctx, ownerID)
	if err != nil {
		return err
	}
	if limit == nil {
		return nil
	}
	if *limit <= 0 {
		return models.ErrCompanyLimitExceeded
	}

	owned, err := s.repository.CountOwnerCompanies(ctx, ownerID)
	if err != nil {
		return err
	}
	if owned >= *limit {
		return models.ErrCompanyLimitExceeded
	}

	return nil
}

// companyLimitFor reads the cap from the owner's business plan, falling back to
// the "free" plan when they have none.
func (s *Service) companyLimitFor(ctx context.Context, ownerID uuid.UUID) (*int, error) {
	subscription, err := s.repository.GetBestActiveBusinessSubscriptionForManager(ctx, ownerID)
	if err == nil {
		return subscription.Plan.CompanyLimit, nil
	}
	if !errors.Is(err, models.ErrSubscriptionNotFound) {
		return nil, err
	}

	free, err := s.repository.GetPlanByCode(ctx, models.PlanCodeFree)
	if err != nil {
		if errors.Is(err, models.ErrPlanNotFound) {
			// A deployment without the "free" plan has nothing to fall back on, and
			// guessing "unlimited" here would hand out companies for nothing.
			zero := 0
			return &zero, nil
		}
		return nil, err
	}

	return free.CompanyLimit, nil
}

func (s *Service) CanCreateDepartment(ctx context.Context, companyID uuid.UUID) error {
	subscription, err := s.activeBusinessSubscription(ctx, companyID)
	if err != nil {
		return err
	}

	// Empty means no cap and zero means none allowed, the same as everywhere
	// else. This was the one limit that read an empty value as a refusal.
	if subscription.Plan.DepartmentsPerCompanyLimit == nil {
		return nil
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
