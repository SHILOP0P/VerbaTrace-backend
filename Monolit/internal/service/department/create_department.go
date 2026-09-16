package department

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) CreateDepartment(ctx context.Context, input models.CreateDepartmentInput) (models.Department, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || input.CompanyUUID == uuid.Nil || input.UserID == uuid.Nil {
		return models.Department{}, models.ErrInvalidDepartmentInput
	}

	member, err := s.companyRepository.GetCompanyMember(ctx, input.CompanyUUID, input.UserID)
	if err != nil {
		return models.Department{}, err
	}

	if !member.Role.ManagesCompany() {
		return models.Department{}, models.ErrForbidden
	}

	if err := s.requireActiveCompanySubscription(ctx, input.CompanyUUID); err != nil {
		return models.Department{}, err
	}

	if s.billingLimiter != nil {
		if err := s.billingLimiter.CanCreateDepartment(ctx, input.CompanyUUID); err != nil {
			return models.Department{}, err
		}
	}

	departmentID, err := uuid.NewV7()
	if err != nil {
		return models.Department{}, err
	}

	department := models.Department{
		ID:          departmentID,
		CompanyUUID: input.CompanyUUID,
		Name:        name,
		CreatedAt:   time.Now().UTC(),
	}

	createdDepartment, err := s.departmentRepository.CreateDepartment(ctx, department)
	if err != nil {
		s.log.Error(ctx, "failed to create department", zap.String("user_id", input.UserID.String()), zap.String("company_id", input.CompanyUUID.String()), zap.Error(err))
		return models.Department{}, err
	}

	s.log.Info(ctx, "department created", zap.String("user_id", input.UserID.String()), zap.String("company_id", input.CompanyUUID.String()), zap.String("department_id", createdDepartment.ID.String()))

	return createdDepartment, nil
}
