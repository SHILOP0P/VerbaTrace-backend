package analysis_instruction

import (
	"context"
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) authorizeCreate(ctx context.Context, input models.CreateAnalysisInstructionInput) error {
	switch input.Scope {
	case models.AnalysisInstructionScopePersonal:
		return nil
	case models.AnalysisInstructionScopeCompany:
		member, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID.UUID, input.CreatedByUserUUID)
		if err != nil {
			return err
		}
		if !member.Role.ManagesCompany() {
			return models.ErrForbidden
		}
		return nil
	case models.AnalysisInstructionScopeDepartment:
		if err := s.authorizeDepartmentManage(ctx, input.CompanyUUID.UUID, input.DepartmentUUID.UUID, input.CreatedByUserUUID); err != nil {
			return err
		}
		return nil
	default:
		return models.ErrInvalidAnalysisInstructionInput
	}
}

func (s *Service) authorizeList(ctx context.Context, input models.ListAnalysisInstructionsInput) error {
	switch input.Scope {
	case models.AnalysisInstructionScopePersonal:
		return nil
	case models.AnalysisInstructionScopeCompany:
		return s.authorizeCompanyInstructionRead(ctx, input.CompanyUUID.UUID, input.UserUUID)
	case models.AnalysisInstructionScopeDepartment:
		companyMember, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID.UUID, input.UserUUID)
		if err == nil && companyMember.Role.ManagesCompany() {
			return nil
		}
		if err != nil && !errors.Is(err, models.ErrCompanyNotFound) {
			return err
		}

		_, err = s.departmentRepository.GetDepartmentMember(ctx, input.CompanyUUID.UUID, input.DepartmentUUID.UUID, input.UserUUID)
		return err
	default:
		return models.ErrInvalidAnalysisInstructionInput
	}
}

func (s *Service) authorizeDelete(ctx context.Context, instruction models.AnalysisInstruction, userID uuid.UUID) error {
	return s.authorizeEdit(ctx, instruction, userID)
}

func (s *Service) authorizeEdit(ctx context.Context, instruction models.AnalysisInstruction, userID uuid.UUID) error {
	switch instruction.Scope {
	case models.AnalysisInstructionScopePersonal:
		if !instruction.UserUUID.Valid || instruction.UserUUID.UUID != userID {
			return models.ErrForbidden
		}
		return nil
	case models.AnalysisInstructionScopeCompany:
		member, err := s.companyRepository.GetCompanyMember(ctx, instruction.CompanyUUID.UUID, userID)
		if err != nil {
			return err
		}
		if !member.Role.ManagesCompany() {
			return models.ErrForbidden
		}
		return nil
	case models.AnalysisInstructionScopeDepartment:
		return s.authorizeDepartmentManage(ctx, instruction.CompanyUUID.UUID, instruction.DepartmentUUID.UUID, userID)
	default:
		return models.ErrInvalidAnalysisInstructionInput
	}
}

func (s *Service) authorizeEditScope(ctx context.Context, input models.ReorderAnalysisInstructionsInput) error {
	switch input.Scope {
	case models.AnalysisInstructionScopePersonal:
		return nil
	case models.AnalysisInstructionScopeCompany:
		member, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID.UUID, input.UserUUID)
		if err != nil {
			return err
		}
		if !member.Role.ManagesCompany() {
			return models.ErrForbidden
		}
		return nil
	case models.AnalysisInstructionScopeDepartment:
		return s.authorizeDepartmentManage(ctx, input.CompanyUUID.UUID, input.DepartmentUUID.UUID, input.UserUUID)
	default:
		return models.ErrInvalidAnalysisInstructionInput
	}
}

func (s *Service) authorizeRead(ctx context.Context, instruction models.AnalysisInstruction, userID uuid.UUID) error {
	switch instruction.Scope {
	case models.AnalysisInstructionScopePersonal:
		if !instruction.UserUUID.Valid || instruction.UserUUID.UUID != userID {
			return models.ErrForbidden
		}
		return nil
	case models.AnalysisInstructionScopeCompany:
		return s.authorizeCompanyInstructionRead(ctx, instruction.CompanyUUID.UUID, userID)
	case models.AnalysisInstructionScopeDepartment:
		companyMember, err := s.companyRepository.GetCompanyMember(ctx, instruction.CompanyUUID.UUID, userID)
		if err == nil && companyMember.Role.ManagesCompany() {
			return nil
		}
		if err != nil && !errors.Is(err, models.ErrCompanyNotFound) {
			return err
		}

		_, err = s.departmentRepository.GetDepartmentMember(ctx, instruction.CompanyUUID.UUID, instruction.DepartmentUUID.UUID, userID)
		return err
	default:
		return models.ErrInvalidAnalysisInstructionInput
	}
}

func (s *Service) authorizeCompanyInstructionRead(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) error {
	member, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err != nil {
		return err
	}
	if member.Role.ManagesCompany() {
		return nil
	}

	departments, err := s.departmentRepository.ListVisibleCompanyDepartments(ctx, companyID, userID)
	if err != nil {
		return err
	}
	if len(departments) == 0 {
		return models.ErrForbidden
	}

	return nil
}

func (s *Service) authorizeDepartmentManage(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID) error {
	companyMember, err := s.companyRepository.GetCompanyMember(ctx, companyID, userID)
	if err == nil && companyMember.Role.ManagesCompany() {
		return nil
	}
	if err != nil && !errors.Is(err, models.ErrCompanyNotFound) {
		return err
	}

	departmentMember, err := s.departmentRepository.GetDepartmentMember(ctx, companyID, departmentID, userID)
	if err != nil {
		return err
	}
	if departmentMember.Role != models.DepartmentMemberRoleLeader {
		return models.ErrForbidden
	}

	return nil
}
