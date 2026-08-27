package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"verbatrace/monolit/internal/auth/password"
	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/username"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const defaultUserRole = model.UserRoleUser

func (s *Service) Register(ctx context.Context, input model.CreateUserInput) (model.CurrentUser, error) {
	input.Email = strings.TrimSpace(strings.ToLower(input.Email))
	input.FullName = strings.TrimSpace(input.FullName)
	input.FullSurname = strings.TrimSpace(input.FullSurname)
	input.Username = strings.TrimSpace(input.Username)
	input.Post = normalizeOptionalString(input.Post)

	if input.Email == "" ||
		input.Password == "" ||
		input.FullName == "" ||
		input.FullSurname == "" {
		return model.CurrentUser{}, model.ErrInvalidUserInput
	}

	if len(input.Password) < 8 {
		return model.CurrentUser{}, model.ErrInvalidUserInput
	}

	_, err := s.userRepository.GetUserByEmail(ctx, input.Email)
	if err == nil {
		return model.CurrentUser{}, model.ErrUserAlreadyExists
	}
	if !errors.Is(err, model.ErrUserNotFound) {
		return model.CurrentUser{}, err
	}

	normalizedUsername, err := s.usernameForNewUser(ctx, input)
	if err != nil {
		return model.CurrentUser{}, err
	}

	passwordHash, err := password.Hash(input.Password, s.passwordPepper)
	if err != nil {
		return model.CurrentUser{}, err
	}

	userID, err := uuid.NewV7()
	if err != nil {
		return model.CurrentUser{}, err
	}

	user := model.CurrentUser{
		ID:           userID,
		Email:        input.Email,
		PasswordHash: passwordHash,
		FullName:     input.FullName,
		FullSurname:  input.FullSurname,
		Username:     normalizedUsername,
		Role:         defaultUserRole,
		Post:         input.Post,
		CreatedAt:    time.Now().UTC(),
	}

	createUser, err := s.userRepository.CreateUser(ctx, user)
	if err != nil {
		return model.CurrentUser{}, err
	}

	if s.billingRepository != nil {
		_, err = s.billingRepository.UpsertSubscription(ctx, model.UpsertSubscriptionInput{
			PlanCode: model.PlanCodePersonalStart,
			UserUUID: uuid.NullUUID{
				UUID:  createUser.ID,
				Valid: true,
			},
			Status:   model.SubscriptionStatusActive,
			StartsAt: time.Now().UTC(),
		})
		if err != nil {
			s.log.Error(ctx, "failed to provision default subscription", zap.String("user_id", createUser.ID.String()), zap.Error(err))
			return model.CurrentUser{}, err
		}
	}

	return createUser, nil
}

func (s *Service) usernameForNewUser(ctx context.Context, input model.CreateUserInput) (string, error) {
	if input.Username != "" {
		normalized, ok := username.Normalize(input.Username)
		if !ok {
			return "", model.ErrInvalidUserInput
		}

		_, err := s.userRepository.GetUserByUsername(ctx, normalized)
		if err == nil {
			return "", model.ErrUserAlreadyExists
		}
		if !errors.Is(err, model.ErrUserNotFound) {
			return "", err
		}

		return normalized, nil
	}

	for range 10 {
		generated, err := username.Generate(input.FullName, input.FullSurname, input.Email)
		if err != nil {
			return "", err
		}

		_, err = s.userRepository.GetUserByUsername(ctx, generated)
		if errors.Is(err, model.ErrUserNotFound) {
			return generated, nil
		}
		if err != nil {
			return "", err
		}
	}

	return "", model.ErrUserAlreadyExists
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}
