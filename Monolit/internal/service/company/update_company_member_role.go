package company

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// UpdateCompanyMemberRole promotes a member to deputy or returns a deputy back
// to a regular membership. Only the owner decides who runs the company, and a
// deputy can never touch another deputy.
func (s *Service) UpdateCompanyMemberRole(ctx context.Context, input models.UpdateCompanyMemberRoleInput) (models.CompanyMember, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if input.RequestUser == input.UserUUID {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if input.Role != models.CompanyMemberRoleEmployee && input.Role != models.CompanyMemberRoleDeputy {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if err := s.requireCompanyOwner(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.CompanyMember{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.CompanyMember{}, err
	}

	target, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.UserUUID)
	if err != nil {
		return models.CompanyMember{}, err
	}
	if target.Role == models.CompanyMemberRoleManager {
		return models.CompanyMember{}, models.ErrOwnerOnlyAction
	}

	if input.Role == models.CompanyMemberRoleDeputy {
		member, err := s.companyRepository.AssignCompanyDeputy(ctx, input.CompanyUUID, input.UserUUID)
		if err != nil {
			s.log.Error(ctx, "failed to assign company deputy", zap.String("company_id", input.CompanyUUID.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
			return models.CompanyMember{}, err
		}
		s.notify(ctx, input.UserUUID, models.NotificationTypeCompanyDeputyAssigned, "Вы назначены заместителем", "Теперь вы управляете компанией вместе с владельцем", "company", input.CompanyUUID)
		s.log.Info(ctx, "company deputy assigned", zap.String("company_id", input.CompanyUUID.String()), zap.String("user_id", input.UserUUID.String()))
		return member, nil
	}

	if target.Role != models.CompanyMemberRoleDeputy {
		return target, nil
	}

	member, err := s.companyRepository.RevokeCompanyDeputy(ctx, input.CompanyUUID)
	if err != nil {
		s.log.Error(ctx, "failed to revoke company deputy", zap.String("company_id", input.CompanyUUID.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
		return models.CompanyMember{}, err
	}
	s.notify(ctx, input.UserUUID, models.NotificationTypeCompanyDeputyRevoked, "Вы больше не заместитель", "Права на управление компанией сняты", "company", input.CompanyUUID)
	s.log.Info(ctx, "company deputy revoked", zap.String("company_id", input.CompanyUUID.String()), zap.String("user_id", input.UserUUID.String()))

	return member, nil
}
