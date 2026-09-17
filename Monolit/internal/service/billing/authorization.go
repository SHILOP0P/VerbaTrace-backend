package billing

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) requireCompanyManager(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	if s.companyRepository == nil {
		return models.ErrForbidden
	}

	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return err
	}

	if !member.Role.ManagesCompany() {
		return models.ErrForbidden
	}

	return nil
}

// requireCompanyOwner guards the subscription itself. The deputy runs the
// company day to day but never touches what the owner pays for.
func (s *Service) requireCompanyOwner(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	if s.companyRepository == nil {
		return models.ErrForbidden
	}

	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return err
	}

	if member.Status != models.MembershipStatusActive || member.Role != models.CompanyMemberRoleManager {
		return models.ErrOwnerOnlyAction
	}

	return nil
}

func (s *Service) requireActiveCompanyMember(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	if s.companyRepository == nil {
		return models.ErrForbidden
	}
	_, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	return err
}
