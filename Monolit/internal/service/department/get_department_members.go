package department

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) ListDepartmentMembers(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID) ([]models.DepartmentMember, error) {
	if companyID == uuid.Nil || departmentID == uuid.Nil || userID == uuid.Nil {
		return nil, models.ErrInvalidDepartmentInput
	}

	if err := s.requireActiveCompanySubscription(ctx, companyID); err != nil {
		return nil, err
	}

	companyMember, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err == nil && companyMember.Role.ManagesCompany() {
		return s.departmentRepository.ListDepartmentMembers(ctx, companyID, departmentID)
	}

	departmentMember, err := s.departmentRepository.GetDepartmentMember(ctx, companyID, departmentID, userID)
	if err != nil {
		return nil, err
	}

	if departmentMember.Role != models.DepartmentMemberRoleLeader {
		return nil, models.ErrForbidden
	}

	return s.departmentRepository.ListDepartmentMembers(ctx, companyID, departmentID)
}
