package billing

import (
	"context"

	"verbatrace/monolit/internal/models"
)

func (s *Service) ListPlans(ctx context.Context) ([]models.Plan, error) {
	return s.repository.ListPlans(ctx)
}
