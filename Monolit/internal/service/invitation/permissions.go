package invitation

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) requireCompanyManager(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return err
	}

	if !member.Role.ManagesCompany() {
		return models.ErrForbidden
	}

	return nil
}

// requireCompanyOwner guards what a deputy must never do, which for invitations
// means offering the deputy seat to somebody else.
func (s *Service) requireCompanyOwner(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return err
	}

	if member.Role != models.CompanyMemberRoleManager {
		return models.ErrOwnerOnlyAction
	}

	return nil
}
