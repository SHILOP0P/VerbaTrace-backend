package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"verbatrace/monolit/internal/auth/refresh"
	"verbatrace/monolit/internal/auth/token"
	model "verbatrace/monolit/internal/models"

	"go.uber.org/zap"
)

func (s *Service) Refresh(ctx context.Context, input model.RefreshTokenInput) (model.CurrentUser, string, string, error) {
	input.RefreshToken = strings.TrimSpace(input.RefreshToken)

	if input.RefreshToken == "" {
		s.log.Warn(ctx, "refresh failed", zap.String("reason", "empty_refresh_token"))
		return model.CurrentUser{}, "", "", model.ErrInvalidRefreshToken
	}

	oldRefreshTokenHash, err := refresh.Hash(input.RefreshToken, s.refreshTokenSecret)
	if err != nil {
		s.log.Error(ctx, "failed to hash refresh token", zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	currentSession, err := s.refreshSessionRepository.GetRefreshSessionByHash(ctx, oldRefreshTokenHash)
	if err != nil {
		if errors.Is(err, model.ErrRefreshSessionNotFound) {
			s.log.Warn(ctx, "refresh failed", zap.String("reason", "session_not_found"))
			return model.CurrentUser{}, "", "", model.ErrInvalidRefreshToken
		}

		s.log.Error(ctx, "failed to get refresh session", zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	now := time.Now().UTC()
	if currentSession.RevokedAt != nil || !currentSession.ExpiresAt.After(now) {
		s.log.Warn(ctx, "refresh failed", zap.String("reason", "inactive_session"), zap.String("user_id", currentSession.UserID.String()), zap.String("session_id", currentSession.ID.String()))
		return model.CurrentUser{}, "", "", model.ErrInvalidRefreshToken
	}

	newRefreshToken, err := refresh.Generate()
	if err != nil {
		s.log.Error(ctx, "failed to generate refresh token", zap.String("user_id", currentSession.UserID.String()), zap.String("session_id", currentSession.ID.String()), zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	newRefreshTokenHash, err := refresh.Hash(newRefreshToken, s.refreshTokenSecret)
	if err != nil {
		s.log.Error(ctx, "failed to hash new refresh token", zap.String("user_id", currentSession.UserID.String()), zap.String("session_id", currentSession.ID.String()), zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	rotatedSession, err := s.refreshSessionRepository.RotateRefreshSession(
		ctx,
		oldRefreshTokenHash,
		newRefreshTokenHash,
		now.Add(s.refreshTokenTTL),
	)
	if err != nil {
		if errors.Is(err, model.ErrRefreshRotationConflict) {
			s.log.Info(ctx, "concurrent refresh already completed", zap.String("session_id", currentSession.ID.String()))
			return model.CurrentUser{}, "", "", model.ErrRefreshRotationConflict
		}

		if errors.Is(err, model.ErrRefreshTokenReuse) {
			s.log.Warn(ctx, "refresh token reuse detected", zap.String("session_id", currentSession.ID.String()))
			return model.CurrentUser{}, "", "", model.ErrInvalidRefreshToken
		}

		if errors.Is(err, model.ErrRefreshSessionNotFound) {
			s.log.Warn(ctx, "refresh failed", zap.String("reason", "session_rotation_failed"), zap.String("user_id", currentSession.UserID.String()), zap.String("session_id", currentSession.ID.String()))
			return model.CurrentUser{}, "", "", model.ErrInvalidRefreshToken
		}

		s.log.Error(ctx, "failed to rotate refresh session", zap.String("user_id", currentSession.UserID.String()), zap.String("session_id", currentSession.ID.String()), zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	user, err := s.userRepository.GetUserByUUID(ctx, rotatedSession.UserID)
	if err != nil {
		s.log.Error(ctx, "failed to get user for refreshed session", zap.String("user_id", rotatedSession.UserID.String()), zap.String("session_id", rotatedSession.ID.String()), zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	accessToken, err := token.GenerateAccessTokenWithSession(
		user.ID,
		rotatedSession.ID,
		string(user.Role),
		s.jwtSecret,
		s.accessTokenTTL,
		rotatedSession.AccessVersion,
	)
	if err != nil {
		s.log.Error(ctx, "failed to generate refreshed access token", zap.String("user_id", user.ID.String()), zap.String("session_id", rotatedSession.ID.String()), zap.Error(err))
		return model.CurrentUser{}, "", "", err
	}

	s.log.Info(ctx, "refresh session rotated", zap.String("user_id", user.ID.String()), zap.String("session_id", rotatedSession.ID.String()))

	return user, accessToken, newRefreshToken, nil
}
