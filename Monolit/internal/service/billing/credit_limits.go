package billing

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// creditLimitRepository is implemented by the billing repository; it stays a
// separate interface so the service keeps working without it in tests.
type creditLimitRepository interface {
	SetCompanyCreditLimit(context.Context, models.SetCreditLimitInput) error
	SetDepartmentCreditLimit(context.Context, models.SetCreditLimitInput) error
	CompanyCreditSpending(context.Context, uuid.UUID, time.Time) (models.CreditSpending, error)
	DepartmentCreditSpending(context.Context, uuid.UUID, time.Time) ([]models.CreditSpending, error)
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

	departments, err := limits.DepartmentCreditSpending(ctx, companyID, s.now())
	if err != nil {
		return models.CompanyCreditForecast{}, err
	}

	if member.Role.ManagesCompany() {
		company, err := limits.CompanyCreditSpending(ctx, companyID, s.now())
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
