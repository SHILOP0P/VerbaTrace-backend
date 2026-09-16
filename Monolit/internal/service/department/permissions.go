package department

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

func validMembershipStatus(status models.MembershipStatus) bool {
	return status == models.MembershipStatusActive ||
		status == models.MembershipStatusLeft
}
