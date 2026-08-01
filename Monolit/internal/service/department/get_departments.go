package department

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) ListCompanyDepartments(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) ([]models.Department, error) {
	if companyID == uuid.Nil || userID == uuid.Nil {
		return nil, models.ErrInvalidDepartmentInput
	}

	if err := s.requireActiveCompanySubscription(ctx, companyID); err != nil {
		return nil, err
	}

	return s.departmentRepository.ListVisibleCompanyDepartments(ctx, companyID, userID)
}
