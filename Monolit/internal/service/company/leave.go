package company

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// LeaveCompany makes an active non-manager member leave together with all department memberships.
// A company manager must first hand management to another user; this operation never silently removes control.
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
	return s.companyRepository.UpdateCompanyMemberStatus(ctx, companyID, userID, models.MembershipStatusLeft)
}
