package auth

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const logoutAllReason = "logout_all"

func (s *Service) LogoutAll(ctx context.Context, userID uuid.UUID, currentSessionID uuid.UUID) error {
	if userID == uuid.Nil || currentSessionID == uuid.Nil {
		return models.ErrInvalidUserInput
	}

	availableAt, err := s.OtherSessionManagementAvailableAt(ctx, userID, currentSessionID)
	if err != nil {
		return err
	}
	if s.now().Before(availableAt) {
		return models.SessionTrustError{AvailableAt: availableAt}
	}

	if err := s.refreshSessionRepository.RevokeAllUserRefreshSessions(ctx, userID, logoutAllReason); err != nil {
		s.log.Error(ctx, "logout all failed", zap.String("user_id", userID.String()), zap.Error(err))
		return err
	}

	s.log.Info(ctx, "user logged out from all sessions", zap.String("user_id", userID.String()))

	return nil
}
