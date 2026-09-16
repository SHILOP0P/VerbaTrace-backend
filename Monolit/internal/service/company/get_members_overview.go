package company

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) GetCompanyMembersOverview(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.CompanyMembersOverview, error) {
	if companyID == uuid.Nil || userID == uuid.Nil {
		return models.CompanyMembersOverview{}, models.ErrInvalidCompanyInput
	}

	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return models.CompanyMembersOverview{}, err
	}

	if !member.Role.ManagesCompany() {
		return models.CompanyMembersOverview{}, models.ErrForbidden
	}

	if err := s.requireActiveCompanySubscription(ctx, companyID); err != nil {
		return models.CompanyMembersOverview{}, err
	}

	return s.companyRepository.GetCompanyMembersOverview(ctx, companyID)
}

func (s *Service) ListCompanyMembers(ctx context.Context, input models.ListCompanyMembersInput) (models.CompanyMembersResult, error) {
	if input.CompanyUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.CompanyMembersResult{}, models.ErrInvalidCompanyInput
	}

	if input.Limit == 0 {
		input.Limit = 20
	}
	if input.Limit < 0 || input.Limit > 100 || input.Offset < 0 {
		return models.CompanyMembersResult{}, models.ErrInvalidCompanyInput
	}

	if input.Status != nil && !validMembershipStatus(*input.Status) {
		return models.CompanyMembersResult{}, models.ErrInvalidCompanyInput
	}

	if input.Role != nil && !validCompanyMemberListRole(*input.Role) {
		return models.CompanyMembersResult{}, models.ErrInvalidCompanyInput
	}

	if err := s.requireCompanyManagement(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.CompanyMembersResult{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.CompanyMembersResult{}, err
	}

	return s.companyRepository.ListCompanyMembers(ctx, input)
}

func validCompanyMemberListRole(role string) bool {
	return role == string(models.CompanyMemberRoleEmployee) ||
		role == string(models.CompanyMemberRoleManager) ||
		role == string(models.CompanyMemberRoleDeputy) ||
		role == string(models.DepartmentMemberRoleLeader)
}
