package company

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) UpdateCompanyMemberStatus(ctx context.Context, input models.UpdateCompanyMemberStatusInput) (models.CompanyMember, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if input.RequestUser == input.UserUUID {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if !validMembershipStatus(input.Status) {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.CompanyMember{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.CompanyMember{}, err
	}

	if input.Status != models.MembershipStatusActive {
		currentMember, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.UserUUID)
		if err != nil {
			return models.CompanyMember{}, err
		}

		if currentMember.Role == models.CompanyMemberRoleManager {
			managerCount, err := s.companyRepository.CountActiveCompanyManagers(ctx, input.CompanyUUID, input.UserUUID)
			if err != nil {
				return models.CompanyMember{}, err
			}
			if managerCount == 0 {
				return models.CompanyMember{}, models.ErrLastCompanyManager
			}
		}
	}

	member, err := s.companyRepository.UpdateCompanyMemberStatus(ctx, input.CompanyUUID, input.UserUUID, input.Status)
	if err != nil {
		s.log.Error(ctx, "failed to update company member status", zap.String("company_id", input.CompanyUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
		return models.CompanyMember{}, err
	}

	s.log.Info(ctx, "company member status updated", zap.String("company_id", input.CompanyUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.String("status", string(member.Status)))

	return member, nil
}

func validMembershipStatus(status models.MembershipStatus) bool {
	return status == models.MembershipStatusActive ||
		status == models.MembershipStatusSuspended ||
		status == models.MembershipStatusLeft
}
