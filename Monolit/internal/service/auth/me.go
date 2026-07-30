package auth

import (
	"context"

	"calllens/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) Me(ctx context.Context, userID uuid.UUID) (models.CurrentUser, error) {
	return s.userRepository.GetUserByUUID(ctx, userID)
}
