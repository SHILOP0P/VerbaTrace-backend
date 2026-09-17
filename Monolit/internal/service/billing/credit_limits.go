package billing

import (
	"context"
	"errors"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// creditLimitRepository is implemented by the billing repository; it stays a
// separate interface so the service keeps working without it in tests.
type creditLimitRepository interface {
	SetCompanyCreditLimit(context.Context, models.SetCreditLimitInput) error
	SetDepartmentCreditLimit(context.Context, models.SetCreditLimitInput) error
	CompanyCreditSpending(context.Context, uuid.UUID, models.CreditPeriod, time.Time) (models.CreditSpending, error)
	DepartmentCreditSpending(context.Context, uuid.UUID, models.CreditPeriod, time.Time) ([]models.CreditSpending, error)
}

type DepartmentRepository interface {
	ListUserDepartments(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) ([]models.CompanyMemberDepartment, error)
}

func (s *Service) SetDepartmentRepository(repository DepartmentRepository) {
	s.departmentRepository = repository
}

// SetCompanyCreditLimit caps what one company may spend out of the credits the
// owner pays for. Only the owner decides that: the deputy splits the cap
// between departments but cannot raise the company's own.
func (s *Service) SetCompanyCreditLimit(ctx context.Context, input models.SetCreditLimitInput) error {
	limits := s.creditLimits
	if limits == nil || input.CompanyUUID == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.ErrInvalidBillingInput
	}
	if input.LimitCredits != nil && *input.LimitCredits < 0 {
		return models.ErrInvalidBillingInput
	}
	member, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.UserUUID)
	if err != nil {
		return models.ErrForbidden
	}
	if member.Status != models.MembershipStatusActive || member.Role != models.CompanyMemberRoleManager {
		return models.ErrOwnerOnlyAction
	}

	return limits.SetCompanyCreditLimit(ctx, input)
}

// SetDepartmentCreditLimit splits the company cap between departments. The
// deputy does this day to day, the owner may do it as well.
func (s *Service) SetDepartmentCreditLimit(ctx context.Context, input models.SetCreditLimitInput) error {
	limits := s.creditLimits
	if limits == nil || input.CompanyUUID == uuid.Nil || !input.DepartmentUUID.Valid || input.UserUUID == uuid.Nil {
		return models.ErrInvalidBillingInput
	}
	if input.LimitCredits != nil && *input.LimitCredits < 0 {
		return models.ErrInvalidBillingInput
	}
	member, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.UserUUID)
	if err != nil {
		return models.ErrForbidden
	}
	if member.Status != models.MembershipStatusActive || !member.Role.ManagesCompany() {
		return models.ErrForbidden
	}

	return limits.SetDepartmentCreditLimit(ctx, input)
}

// CompanyCreditForecast shows how the period is going: the owner and the deputy
// see the whole company with a line per department, a leader sees their own
// department only.
func (s *Service) CompanyCreditForecast(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.CompanyCreditForecast, error) {
	limits := s.creditLimits
	if limits == nil || companyID == uuid.Nil || userID == uuid.Nil {
		return models.CompanyCreditForecast{}, models.ErrInvalidBillingInput
	}
	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil || member.Status != models.MembershipStatusActive {
		return models.CompanyCreditForecast{}, models.ErrForbidden
	}

	// The window follows the owner's subscription, so the cap resets together
	// with the allowance it caps rather than on the first of the month.
	now := s.now()
	period, err := s.creditPeriodForCompany(ctx, companyID, now)
	if err != nil {
		return models.CompanyCreditForecast{}, err
	}

	departments, err := limits.DepartmentCreditSpending(ctx, companyID, period, now)
	if err != nil {
		return models.CompanyCreditForecast{}, err
	}

	if member.Role.ManagesCompany() {
		company, err := limits.CompanyCreditSpending(ctx, companyID, period, now)
		if err != nil {
			return models.CompanyCreditForecast{}, err
		}
		return models.CompanyCreditForecast{Company: company, Departments: departments}, nil
	}

	if s.departmentRepository == nil {
		return models.CompanyCreditForecast{}, models.ErrForbidden
	}
	own, err := s.departmentRepository.ListUserDepartments(ctx, companyID, userID)
	if err != nil {
		return models.CompanyCreditForecast{}, err
	}
	led := map[uuid.UUID]struct{}{}
	for _, department := range own {
		if department.Role == models.DepartmentMemberRoleLeader {
			led[department.DepartmentUUID] = struct{}{}
		}
	}
	if len(led) == 0 {
		return models.CompanyCreditForecast{}, models.ErrForbidden
	}

	visible := make([]models.CreditSpending, 0, len(led))
	for _, department := range departments {
		if _, ok := led[department.SubjectUUID]; ok {
			visible = append(visible, department)
		}
	}

	return models.CompanyCreditForecast{Departments: visible}, nil
}

// creditPeriodForCompany anchors the window on the plan that covers the company.
// A frozen company has no covering plan; its numbers are read from a window
// starting now, which is empty, and that is the honest answer for a company that
// is not allowed to spend.
func (s *Service) creditPeriodForCompany(ctx context.Context, companyID uuid.UUID, now time.Time) (models.CreditPeriod, error) {
	subscription, err := s.repository.GetActiveBusinessSubscription(ctx, companyID)
	if err != nil {
		if errors.Is(err, models.ErrSubscriptionNotFound) {
			return models.CreditPeriodFor(time.Time{}, now), nil
		}
		return models.CreditPeriod{}, err
	}

	return models.CreditPeriodFor(subscription.StartsAt, now), nil
}
