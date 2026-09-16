package company

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// LeaveCompany lets a member or a deputy walk away and lose everything the
// company gave them. The owner cannot leave: the company is theirs until they
// hand ownership over to somebody else.
func (s *Service) LeaveCompany(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.CompanyMember, error) {
	if companyID == uuid.Nil || userID == uuid.Nil {
		return models.CompanyMember{}, models.ErrInvalidCompanyInput
	}

	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return models.CompanyMember{}, err
	}

	if member.Role == models.CompanyMemberRoleManager {
		return models.CompanyMember{}, models.ErrLastCompanyManager
	}

	return s.companyRepository.RemoveCompanyMember(ctx, companyID, userID, time.Now().UTC())
}
