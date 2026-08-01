package auth

import (
	"context"
	"errors"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/username"

	"github.com/google/uuid"
)

func (s *Service) UpdateUsername(ctx context.Context, input models.UpdateUsernameInput) (models.CurrentUser, error) {
	if input.UserUUID == uuid.Nil {
		return models.CurrentUser{}, models.ErrInvalidUserInput
	}

	normalized, ok := username.Normalize(input.Username)
	if !ok {
		return models.CurrentUser{}, models.ErrInvalidUserInput
	}

	existing, err := s.userRepository.GetUserByUsername(ctx, normalized)
	if err == nil && existing.ID != input.UserUUID {
		return models.CurrentUser{}, models.ErrUserAlreadyExists
	}
	if err != nil && !errors.Is(err, models.ErrUserNotFound) {
		return models.CurrentUser{}, err
	}

	return s.userRepository.UpdateUsername(ctx, models.UpdateUsernameInput{
		UserUUID: input.UserUUID,
		Username: normalized,
	})
}

func (s *Service) GetUserByUsername(ctx context.Context, value string) (models.CurrentUser, error) {
	normalized, ok := username.Normalize(value)
	if !ok {
		return models.CurrentUser{}, models.ErrInvalidUserInput
	}

	return s.userRepository.GetUserByUsername(ctx, normalized)
}
