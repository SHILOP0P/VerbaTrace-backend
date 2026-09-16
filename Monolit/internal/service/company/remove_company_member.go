package company

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// membershipRestrictionTTL keeps an exclusion visible long enough to stop a
// department leader from quietly inviting the person back, and short enough to
// not pile up forever.
const membershipRestrictionTTL = 180 * 24 * time.Hour

// RemoveCompanyMember excludes a member. Leaving on your own goes through
// LeaveCompany and ends in the same repository call.
func (s *Service) RemoveCompanyMember(ctx context.Context, input models.RemoveCompanyMemberInput) (models.CompanyMember, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if input.RequestUser == input.UserUUID {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	if err := s.requireCompanyManagement(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.CompanyMember{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.CompanyMember{}, err
	}

	target, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.UserUUID)
	if err != nil {
		return models.CompanyMember{}, err
	}

	// Neither the owner nor a deputy can be pushed out by somebody with the same
	// or lower rank: the owner is permanent and deputies are the owner's call.
	if target.Role == models.CompanyMemberRoleManager {
		return models.CompanyMember{}, models.ErrLastCompanyManager
	}
	if target.Role == models.CompanyMemberRoleDeputy {
		if err := s.requireCompanyOwner(ctx, input.CompanyUUID, input.RequestUser); err != nil {
			return models.CompanyMember{}, err
		}
	}

	now := time.Now().UTC()
	member, err := s.companyRepository.RemoveCompanyMember(ctx, input.CompanyUUID, input.UserUUID, now)
	if err != nil {
		s.log.Error(ctx, "failed to remove company member", zap.String("company_id", input.CompanyUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
		return models.CompanyMember{}, err
	}

	restriction := models.CompanyMembershipRestriction{
		ID:                uuid.New(),
		CompanyUUID:       input.CompanyUUID,
		UserUUID:          input.UserUUID,
		Kind:              models.CompanyRestrictionExcludedByManager,
		CreatedByUserUUID: input.RequestUser,
		CreatedAt:         now,
		ExpiresAt:         now.Add(membershipRestrictionTTL),
	}
	if reason := strings.TrimSpace(input.Reason); reason != "" {
		restriction.Reason = &reason
	}
	if err := s.companyRepository.UpsertMembershipRestriction(ctx, restriction); err != nil {
		s.log.Error(ctx, "failed to record membership restriction", zap.String("company_id", input.CompanyUUID.String()), zap.String("user_id", input.UserUUID.String()), zap.Error(err))
		return models.CompanyMember{}, err
	}

	s.notify(ctx, input.UserUUID, models.NotificationTypeCompanyMemberRemoved, "Вы исключены из компании", "Доступ к данным компании закрыт", "company", input.CompanyUUID)
	s.log.Info(ctx, "company member removed", zap.String("company_id", input.CompanyUUID.String()), zap.String("request_user_id", input.RequestUser.String()), zap.String("user_id", input.UserUUID.String()))

	return member, nil
}
