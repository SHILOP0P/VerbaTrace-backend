package auth

import (
	"context"
	"strings"
	"time"

	"verbatrace/monolit/internal/auth/password"
	"verbatrace/monolit/internal/auth/refresh"
	"verbatrace/monolit/internal/auth/token"
	model "verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func (s *Service) Login(ctx context.Context, input model.LoginInput) (model.CurrentUser, string, string, error) {
	email := strings.TrimSpace(strings.ToLower(input.Email))

	if email == "" || input.Password == "" {
		s.log.Warn(ctx, "login failed", zap.String("reason", "empty_credentials"))
		return model.CurrentUser{}, "", "", model.ErrInvalidCredentials
	}

	user, err := s.userRepository.GetUserByEmail(ctx, email)
	if err != nil {
		s.log.Warn(ctx, "login failed", zap.String("reason", "invalid_credentials"), zap.Error(err))
		return model.CurrentUser{}, "", "", model.ErrInvalidCredentials
	}

	if err := password.Compare(input.Password, user.PasswordHash, s.passwordPepper); err != nil {
		s.log.Warn(ctx, "login failed", zap.String("reason", "invalid_credentials"), zap.String("user_id", user.ID.String()))
		return model.CurrentUser{}, "", "", model.ErrInvalidCredentials
	}

	refreshToken, err := refresh.Generate()
	if err != nil {
		s.log.Error(ctx, "failed to generate refresh token", zap.String("user_id", user.ID.String()), zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	refreshTokenHash, err := refresh.Hash(refreshToken, s.refreshTokenSecret)
	if err != nil {
		s.log.Error(ctx, "failed to hash refresh token", zap.String("user_id", user.ID.String()), zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	sessionID, err := uuid.NewV7()
	if err != nil {
		s.log.Error(ctx, "failed to generate refresh session uuid", zap.String("user_id", user.ID.String()), zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	now := time.Now().UTC()
	session := model.RefreshSession{
		ID:               sessionID,
		UserID:           user.ID,
		RefreshTokenHash: refreshTokenHash,
		AccessVersion:    1,
		UserAgent:        input.UserAgent,
		IPAddress:        input.IPAddress,
		CreatedAt:        now,
		ExpiresAt:        now.Add(s.refreshTokenTTL),
	}

	createdSession, err := s.refreshSessionRepository.CreateRefreshSession(ctx, session)
	if err != nil {
		s.log.Error(ctx, "failed to create refresh session", zap.String("user_id", user.ID.String()), zap.String("session_id", sessionID.String()), zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	accessToken, err := token.GenerateAccessTokenWithSession(
		user.ID,
		createdSession.ID,
		string(user.Role),
		s.jwtSecret,
		s.accessTokenTTL,
		createdSession.AccessVersion,
	)
	if err != nil {
		s.log.Error(ctx, "failed to generate access token", zap.String("user_id", user.ID.String()), zap.String("session_id", createdSession.ID.String()), zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	s.log.Info(ctx, "user logged in", zap.String("user_id", user.ID.String()), zap.String("session_id", createdSession.ID.String()))

	return user, accessToken, refreshToken, nil
}
