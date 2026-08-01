package company

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) UpdateCompanyMemberRole(ctx context.Context, input models.UpdateCompanyMemberRoleInput) (models.CompanyMember, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if input.RequestUser == input.UserUUID {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if input.Role != models.CompanyMemberRoleEmployee {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.CompanyMember{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.CompanyMember{}, err
	}

	member, err := s.companyRepository.UpdateCompanyMemberRole(ctx, input.CompanyUUID, input.UserUUID, input.Role)
	if err != nil {
		s.log.Error(ctx, "failed to update company member role", zap.String("company_id", input.CompanyUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
		return models.CompanyMember{}, err
	}

	s.log.Info(ctx, "company member role updated", zap.String("company_id", input.CompanyUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.String("role", string(member.Role)))

	return member, nil
}
