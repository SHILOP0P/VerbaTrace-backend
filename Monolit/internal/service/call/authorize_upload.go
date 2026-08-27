package call

import (
	"context"
	"errors"

	"verbatrace/monolit/internal/models"
)

func (s *Service) authorizeUpload(ctx context.Context, input models.CreateCallInput) error {
	if input.IntegrationPrincipalUUID.Valid {
		return nil
	}
	switch input.VisibilityScope {
	case models.CallVisibilityScopePersonal:
		return nil
	case models.CallVisibilityScopeCompany:
		return s.authorizeCompanyUpload(ctx, input)
	case models.CallVisibilityScopeDepartment:
		return s.authorizeDepartmentUpload(ctx, input)
	default:
		return models.ErrInvalidCallPlacement
	}
}

func (s *Service) authorizeCompanyUpload(ctx context.Context, input models.CreateCallInput) error {
	member, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID.UUID, input.UploadedByUserUUID)
	if err != nil {
		return models.ErrForbidden
	}

	if member.Role != models.CompanyMemberRoleManager {
		return models.ErrForbidden
	}

	return nil
}

func (s *Service) authorizeDepartmentUpload(ctx context.Context, input models.CreateCallInput) error {
	companyMember, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID.UUID, input.UploadedByUserUUID)
	if err == nil && companyMember.Role == models.CompanyMemberRoleManager {
		return nil
	}

	if err != nil && !errors.Is(err, models.ErrCompanyNotFound) {
		return models.ErrForbidden
	}

	departmentMember, err := s.departmentRepository.GetDepartmentMember(ctx, input.CompanyUUID.UUID, input.DepartmentUUID.UUID, input.UploadedByUserUUID)
	if err != nil {
		return models.ErrForbidden
	}

	if departmentMember.Role != models.DepartmentMemberRoleLeader && departmentMember.Role != models.DepartmentMemberRoleEmployee {
		return models.ErrForbidden
	}

	return nil
}
