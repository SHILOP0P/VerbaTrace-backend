package company

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) AddCompanyMember(ctx context.Context, input models.AddCompanyMemberInput) (models.CompanyMember, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if input.RequestUser == input.UserUUID {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if input.Role != models.CompanyMemberRoleEmployee {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	requestMember, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.RequestUser)
	if err != nil {
		return models.CompanyMember{}, err
	}

	if requestMember.Role != models.CompanyMemberRoleManager {
		return models.CompanyMember{}, models.ErrForbidden
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.CompanyMember{}, err
	}

	if s.billingLimiter != nil {
		if err := s.billingLimiter.CanAddCompanyMember(ctx, input.CompanyUUID); err != nil {
			return models.CompanyMember{}, err
		}
	}

	member := models.CompanyMember{
		CompanyUUID: input.CompanyUUID,
		UserUUID:    input.UserUUID,
		Role:        input.Role,
		Status:      models.MembershipStatusActive,
		CreatedAt:   time.Now().UTC(),
	}

	createdMember, err := s.companyRepository.AddCompanyMember(ctx, member)
	if err != nil {
		s.log.Error(ctx, "failed to add company member", zap.String("company_id", input.CompanyUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
		return models.CompanyMember{}, err
	}

	s.log.Info(ctx, "company member added", zap.String("company_id", input.CompanyUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.String("role", string(createdMember.Role)))

	return createdMember, nil
}
