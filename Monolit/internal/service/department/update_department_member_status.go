package department

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) UpdateDepartmentMemberStatus(ctx context.Context, input models.UpdateDepartmentMemberStatusInput) (models.DepartmentMember, error) {
	if input.CompanyUUID == uuid.Nil || input.DepartmentUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.DepartmentMember{}, models.ErrInvalidDepartmentInput
	}

	if !validMembershipStatus(input.Status) {
		return models.DepartmentMember{}, models.ErrInvalidDepartmentInput
	}

	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.DepartmentMember{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.DepartmentMember{}, err
	}

	member, err := s.departmentRepository.UpdateDepartmentMemberStatus(ctx, input.CompanyUUID, input.DepartmentUUID, input.UserUUID, input.Status)
	if err != nil {
		s.log.Error(ctx, "failed to update department member status", zap.String("company_id", input.CompanyUUID.String()), zap.String("department_id", input.DepartmentUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
		return models.DepartmentMember{}, err
	}

	s.log.Info(ctx, "department member status updated", zap.String("company_id", input.CompanyUUID.String()), zap.String("department_id", input.DepartmentUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.String("status", string(member.Status)))

	return member, nil
}
