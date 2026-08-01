package company

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) GetCompanyByUUID(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.Company, error) {
	if companyID == uuid.Nil || userID == uuid.Nil {
		return models.Company{}, models.ErrInvalidCompanyInput
	}

	return s.companyRepository.GetCompanyByUUID(ctx, companyID, userID)
}
