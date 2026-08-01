package department

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) AddDepartmentMember(ctx context.Context, input models.AddDepartmentMemberInput) (models.DepartmentMember, error) {
	if input.CompanyUUID == uuid.Nil || input.DepartmentUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.DepartmentMember{}, models.ErrInvalidDepartmentInput
	}

	if input.Role != models.DepartmentMemberRoleLeader && input.Role != models.DepartmentMemberRoleEmployee {
		return models.DepartmentMember{}, models.ErrInvalidDepartmentInput
	}

	if err := s.authorizeAddDepartmentMember(ctx, input); err != nil {
		return models.DepartmentMember{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.DepartmentMember{}, err
	}

	if _, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.UserUUID); err != nil {
		return models.DepartmentMember{}, err
	}

	if s.billingLimiter != nil {
		if err := s.billingLimiter.CanAddCompanyMember(ctx, input.CompanyUUID); err != nil {
			return models.DepartmentMember{}, err
		}
	}

	member := models.DepartmentMember{
		DepartmentUUID: input.DepartmentUUID,
		UserUUID:       input.UserUUID,
		Role:           input.Role,
		Status:         models.MembershipStatusActive,
		CreatedAt:      time.Now().UTC(),
	}

	createdMember, err := s.departmentRepository.AddDepartmentMember(ctx, input.CompanyUUID, member)
	if err != nil {
		s.log.Error(ctx, "failed to add department member", zap.String("company_id", input.CompanyUUID.String()), zap.String("department_id", input.DepartmentUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
		return models.DepartmentMember{}, err
	}

	s.log.Info(ctx, "department member added", zap.String("company_id", input.CompanyUUID.String()), zap.String("department_id", input.DepartmentUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.String("role", string(createdMember.Role)))

	return createdMember, nil
}

func (s *Service) authorizeAddDepartmentMember(ctx context.Context, input models.AddDepartmentMemberInput) error {
	requestMember, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.RequestUser)
	if err != nil {
		return err
	}
	if requestMember.Role == models.CompanyMemberRoleManager {
		return nil
	}

	if input.Role != models.DepartmentMemberRoleEmployee {
		return models.ErrForbidden
	}

	departmentMember, err := s.departmentRepository.GetDepartmentMember(ctx, input.CompanyUUID, input.DepartmentUUID, input.RequestUser)
	if err != nil {
		return err
	}
	if departmentMember.Role != models.DepartmentMemberRoleLeader {
		return models.ErrForbidden
	}

	return nil
}
