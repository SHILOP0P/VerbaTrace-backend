package department

import (
	"context"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) UpdateDepartment(ctx context.Context, input models.UpdateDepartmentInput) (models.Department, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.CompanyUUID == uuid.Nil || input.DepartmentUUID == uuid.Nil || input.RequestUser == uuid.Nil || input.Name == "" {
		return models.Department{}, models.ErrInvalidDepartmentInput
	}

	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return models.Department{}, err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.Department{}, err
	}

	return s.departmentRepository.UpdateDepartment(ctx, input.CompanyUUID, input.DepartmentUUID, input.Name)
}

func (s *Service) DeleteDepartment(ctx context.Context, input models.DeleteDepartmentInput) error {
	if input.CompanyUUID == uuid.Nil || input.DepartmentUUID == uuid.Nil || input.RequestUser == uuid.Nil {
		return models.ErrInvalidDepartmentInput
	}

	if err := s.requireCompanyManager(ctx, input.CompanyUUID, input.RequestUser); err != nil {
		return err
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return err
	}

	return s.departmentRepository.ArchiveDepartment(ctx, input.CompanyUUID, input.DepartmentUUID)
}
