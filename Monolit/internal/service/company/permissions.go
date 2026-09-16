package company

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// requireCompanyManagement allows the owner and the company deputy. Everything
// that is not in the owner-only list goes through this check.
func (s *Service) requireCompanyManagement(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return err
	}

	if !member.Role.ManagesCompany() {
		return models.ErrForbidden
	}

	return nil
}

// requireCompanyOwner guards the actions a deputy must never perform: deleting
// or freezing the company, subscription and limits, deputies, ownership
// transfer, company identity, integration revocation and support access.
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

func validMembershipStatus(status models.MembershipStatus) bool {
	return status == models.MembershipStatusActive || status == models.MembershipStatusLeft
}
