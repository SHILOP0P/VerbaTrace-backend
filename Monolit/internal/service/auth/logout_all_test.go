package auth

import (
	"errors"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *ServiceSuite) TestLogoutAllSuccess() {
	userID := uuid.New()
	sessionID := uuid.New()
	now := time.Now().UTC()
	s.service.now = func() time.Time { return now }
	s.refreshSessionRepository.On("GetRefreshSessionByUUID", s.ctx, sessionID).
		Return(models.RefreshSession{ID: sessionID, UserID: userID, CreatedAt: now.Add(-25 * time.Hour), ExpiresAt: now.Add(time.Hour)}, nil).
		Once()

	s.refreshSessionRepository.On("RevokeAllUserRefreshSessions", s.ctx, userID, logoutAllReason).
		Return(nil).
		Once()

	err := s.service.LogoutAll(s.ctx, userID, sessionID)

	s.Require().NoError(err)
}

func (s *ServiceSuite) TestLogoutAllReturnsRepositoryError() {
	userID := uuid.New()
	sessionID := uuid.New()
	repoErr := errors.New("db failed")
	now := time.Now().UTC()
	s.service.now = func() time.Time { return now }
	s.refreshSessionRepository.On("GetRefreshSessionByUUID", s.ctx, sessionID).
		Return(models.RefreshSession{ID: sessionID, UserID: userID, CreatedAt: now.Add(-25 * time.Hour), ExpiresAt: now.Add(time.Hour)}, nil).
		Once()

	s.refreshSessionRepository.On("RevokeAllUserRefreshSessions", s.ctx, userID, logoutAllReason).
		Return(repoErr).
		Once()

	err := s.service.LogoutAll(s.ctx, userID, sessionID)

	s.Require().ErrorIs(err, repoErr)
}

func (s *ServiceSuite) TestLogoutAllRequiresTrustedCurrentSession() {
	userID := uuid.New()
	sessionID := uuid.New()
	now := time.Now().UTC()
	s.service.now = func() time.Time { return now }
	s.refreshSessionRepository.On("GetRefreshSessionByUUID", s.ctx, sessionID).
		Return(models.RefreshSession{ID: sessionID, UserID: userID, CreatedAt: now.Add(-23 * time.Hour), ExpiresAt: now.Add(time.Hour)}, nil).
		Once()

	err := s.service.LogoutAll(s.ctx, userID, sessionID)

	s.Require().ErrorIs(err, models.ErrSessionNotTrusted)
	var trustErr models.SessionTrustError
	s.Require().ErrorAs(err, &trustErr)
	s.Require().True(trustErr.AvailableAt.Equal(now.Add(time.Hour)))
}

func (s *ServiceSuite) TestLogoutAllAllowsExactTrustBoundary() {
	userID := uuid.New()
	sessionID := uuid.New()
	now := time.Now().UTC()
	s.service.now = func() time.Time { return now }
	s.refreshSessionRepository.On("GetRefreshSessionByUUID", s.ctx, sessionID).
		Return(models.RefreshSession{ID: sessionID, UserID: userID, CreatedAt: now.Add(-24 * time.Hour), ExpiresAt: now.Add(time.Hour)}, nil).
		Once()
	s.refreshSessionRepository.On("RevokeAllUserRefreshSessions", s.ctx, userID, logoutAllReason).
		Return(nil).
		Once()

	err := s.service.LogoutAll(s.ctx, userID, sessionID)

	s.Require().NoError(err)
}
