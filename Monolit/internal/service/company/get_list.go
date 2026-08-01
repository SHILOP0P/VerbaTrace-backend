package company

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) ListUserCompanies(ctx context.Context, userID uuid.UUID) ([]models.Company, error) {
	if userID == uuid.Nil {
		return nil, models.ErrInvalidUserInput
	}

	return s.companyRepository.ListUserCompanies(ctx, userID)
}
