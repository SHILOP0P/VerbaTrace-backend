package call

import (
	"context"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) UpdateCallTitle(ctx context.Context, id uuid.UUID, userID uuid.UUID, title string) (models.Call, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return models.Call{}, models.ErrInvalidCallTitle
	}

	return s.repository.UpdateCallTitle(ctx, id, userID, title)
}
