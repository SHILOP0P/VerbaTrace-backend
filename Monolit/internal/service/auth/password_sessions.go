package auth

import (
	"context"
	"time"

	"verbatrace/monolit/internal/auth/password"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const passwordChangedReason = "password_changed"

func (s *Service) UpdatePassword(ctx context.Context, input models.UpdatePasswordInput) (models.UpdatePasswordResult, error) {
	if input.UserUUID == uuid.Nil || input.SessionUUID == uuid.Nil || input.CurrentPassword == "" || input.NewPassword == "" {
		return models.UpdatePasswordResult{}, models.ErrInvalidUserInput
	}
	if len(input.NewPassword) < 8 {
		return models.UpdatePasswordResult{}, models.ErrInvalidUserInput
	}

	user, err := s.userRepository.GetUserByUUID(ctx, input.UserUUID)
	if err != nil {
		return models.UpdatePasswordResult{}, err
	}

	if err := password.Compare(input.CurrentPassword, user.PasswordHash, s.passwordPepper); err != nil {
		return models.UpdatePasswordResult{}, models.ErrInvalidCredentials
	}

	passwordHash, err := password.Hash(input.NewPassword, s.passwordPepper)
	if err != nil {
		return models.UpdatePasswordResult{}, err
	}

	if _, err := s.userRepository.UpdatePasswordHash(ctx, input.UserUUID, passwordHash); err != nil {
		return models.UpdatePasswordResult{}, err
	}

	if err := s.refreshSessionRepository.RevokeOtherUserRefreshSessions(ctx, input.UserUUID, input.SessionUUID, passwordChangedReason); err != nil {
		s.log.Error(ctx, "failed to revoke other sessions after password change", zap.String("user_id", input.UserUUID.String()), zap.String("session_id", input.SessionUUID.String()), zap.Error(err))
		return models.UpdatePasswordResult{}, err
	}

	return models.UpdatePasswordResult{UpdatedAt: time.Now().UTC()}, nil
}

func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID, currentSessionID uuid.UUID) ([]models.UserSession, error) {
	if userID == uuid.Nil || currentSessionID == uuid.Nil {
		return nil, models.ErrInvalidUserInput
	}

	refreshSessions, err := s.refreshSessionRepository.ListActiveUserRefreshSessions(ctx, userID)
	if err != nil {
		return nil, err
	}

	sessions := make([]models.UserSession, 0, len(refreshSessions))
	for _, refreshSession := range refreshSessions {
		lastSeenAt := refreshSession.LastUsedAt
		if lastSeenAt == nil {
			createdAt := refreshSession.CreatedAt
			lastSeenAt = &createdAt
		}

		sessions = append(sessions, models.UserSession{
			ID:         refreshSession.ID,
			Current:    refreshSession.ID == currentSessionID,
			UserAgent:  refreshSession.UserAgent,
			IPAddress:  refreshSession.IPAddress,
			CreatedAt:  refreshSession.CreatedAt,
			LastSeenAt: lastSeenAt,
		})
	}

	return sessions, nil
}

func (s *Service) RevokeSession(ctx context.Context, userID uuid.UUID, currentSessionID uuid.UUID, sessionID uuid.UUID) error {
	if userID == uuid.Nil || currentSessionID == uuid.Nil || sessionID == uuid.Nil {
		return models.ErrInvalidUserInput
	}

	if sessionID != currentSessionID {
		availableAt, err := s.OtherSessionManagementAvailableAt(ctx, userID, currentSessionID)
		if err != nil {
			return err
		}
		if s.now().Before(availableAt) {
			return models.SessionTrustError{AvailableAt: availableAt}
		}
	}

	return s.refreshSessionRepository.RevokeUserRefreshSession(ctx, userID, sessionID, logoutReason)
}

// OtherSessionManagementAvailableAt returns the first time the current session may revoke other sessions.
func (s *Service) OtherSessionManagementAvailableAt(ctx context.Context, userID uuid.UUID, currentSessionID uuid.UUID) (time.Time, error) {
	if userID == uuid.Nil || currentSessionID == uuid.Nil {
		return time.Time{}, models.ErrInvalidUserInput
	}
	currentSession, err := s.refreshSessionRepository.GetRefreshSessionByUUID(ctx, currentSessionID)
	if err != nil {
		return time.Time{}, err
	}
	if currentSession.UserID != userID || currentSession.RevokedAt != nil || !currentSession.ExpiresAt.After(s.now()) {
		return time.Time{}, models.ErrRefreshSessionNotFound
	}
	return currentSession.CreatedAt.Add(s.sessionTrustAge), nil
}
