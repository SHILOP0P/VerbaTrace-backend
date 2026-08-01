package auth

import (
	"testing"
	"time"

	"verbatrace/monolit/internal/auth/password"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestUpdatePasswordValidationAndSessionRevocation(t *testing.T) {
	s := new(ServiceSuite)
	s.SetT(t)
	s.SetupTest()

	userID := uuid.New()
	sessionID := uuid.New()
	hash, err := password.Hash("old-password", "password-pepper")
	require.NoError(t, err)

	s.userRepository.On("GetUserByUUID", s.ctx, userID).
		Return(models.CurrentUser{ID: userID, PasswordHash: hash}, nil).
		Once()

	_, err = s.service.UpdatePassword(s.ctx, models.UpdatePasswordInput{
		UserUUID:        userID,
		SessionUUID:     sessionID,
		CurrentPassword: "wrong-password",
		NewPassword:     "new-password",
	})
	require.ErrorIs(t, err, models.ErrInvalidCredentials)

	_, err = s.service.UpdatePassword(s.ctx, models.UpdatePasswordInput{
		UserUUID:        userID,
		SessionUUID:     sessionID,
		CurrentPassword: "old-password",
		NewPassword:     "short",
	})
	require.ErrorIs(t, err, models.ErrInvalidUserInput)
}

func TestUpdatePasswordChangesHashAndRevokesOtherSessions(t *testing.T) {
	s := new(ServiceSuite)
	s.SetT(t)
	s.SetupTest()

	userID := uuid.New()
	sessionID := uuid.New()
	oldHash, err := password.Hash("old-password", "password-pepper")
	require.NoError(t, err)

	s.userRepository.On("GetUserByUUID", s.ctx, userID).
		Return(models.CurrentUser{ID: userID, PasswordHash: oldHash}, nil).
		Once()
	s.userRepository.On("UpdatePasswordHash", s.ctx, userID, mock.MatchedBy(func(newHash string) bool {
		return newHash != "" &&
			newHash != oldHash &&
			password.Compare("new-password", newHash, "password-pepper") == nil
	})).Return(models.CurrentUser{ID: userID}, nil).Once()
	s.refreshSessionRepository.On("RevokeOtherUserRefreshSessions", s.ctx, userID, sessionID, passwordChangedReason).
		Return(nil).
		Once()

	result, err := s.service.UpdatePassword(s.ctx, models.UpdatePasswordInput{
		UserUUID:        userID,
		SessionUUID:     sessionID,
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	})

	require.NoError(t, err)
	require.False(t, result.UpdatedAt.IsZero())
}

func TestListSessionsMarksCurrentAndHidesRefreshTokenHash(t *testing.T) {
	s := new(ServiceSuite)
	s.SetT(t)
	s.SetupTest()

	userID := uuid.New()
	currentID := uuid.New()
	otherID := uuid.New()
	userAgent := "Chrome on Windows"
	ipAddress := "127.0.0.1/32"
	createdAt := time.Now().UTC().Add(-time.Hour)
	lastUsedAt := time.Now().UTC()

	s.refreshSessionRepository.On("ListActiveUserRefreshSessions", s.ctx, userID).
		Return([]models.RefreshSession{
			{
				ID:               currentID,
				UserID:           userID,
				RefreshTokenHash: "secret-hash",
				UserAgent:        &userAgent,
				IPAddress:        &ipAddress,
				CreatedAt:        createdAt,
				LastUsedAt:       &lastUsedAt,
			},
			{
				ID:        otherID,
				UserID:    userID,
				CreatedAt: createdAt,
			},
		}, nil).
		Once()

	sessions, err := s.service.ListSessions(s.ctx, userID, currentID)

	require.NoError(t, err)
	require.Len(t, sessions, 2)
	require.Equal(t, currentID, sessions[0].ID)
	require.True(t, sessions[0].Current)
	require.Equal(t, &userAgent, sessions[0].UserAgent)
	require.Equal(t, &ipAddress, sessions[0].IPAddress)
	require.Equal(t, &lastUsedAt, sessions[0].LastSeenAt)
	require.Equal(t, otherID, sessions[1].ID)
	require.False(t, sessions[1].Current)
	require.NotNil(t, sessions[1].LastSeenAt)
	require.Equal(t, createdAt, *sessions[1].LastSeenAt)
}

func TestRevokeSessionUsesOwnerScopedRepositoryMethod(t *testing.T) {
	s := new(ServiceSuite)
	s.SetT(t)
	s.SetupTest()

	userID := uuid.New()
	sessionID := uuid.New()

	s.refreshSessionRepository.On("RevokeUserRefreshSession", s.ctx, userID, sessionID, logoutReason).
		Return(models.ErrRefreshSessionNotFound).
		Once()

	err := s.service.RevokeSession(s.ctx, userID, sessionID, sessionID)

	require.ErrorIs(t, err, models.ErrRefreshSessionNotFound)
}

func TestRevokeOtherSessionRequiresTrustedCurrentSession(t *testing.T) {
	s := new(ServiceSuite)
	s.SetT(t)
	s.SetupTest()

	userID := uuid.New()
	currentSessionID := uuid.New()
	targetSessionID := uuid.New()
	now := time.Now().UTC()
	s.service.now = func() time.Time { return now }
	s.refreshSessionRepository.On("GetRefreshSessionByUUID", s.ctx, currentSessionID).
		Return(models.RefreshSession{
			ID:        currentSessionID,
			UserID:    userID,
			CreatedAt: now.Add(-time.Hour),
			ExpiresAt: now.Add(time.Hour),
		}, nil).
		Once()

	err := s.service.RevokeSession(s.ctx, userID, currentSessionID, targetSessionID)

	require.ErrorIs(t, err, models.ErrSessionNotTrusted)
}

func TestRevokeCurrentSessionDoesNotRequireTrustAge(t *testing.T) {
	s := new(ServiceSuite)
	s.SetT(t)
	s.SetupTest()

	userID := uuid.New()
	sessionID := uuid.New()
	s.refreshSessionRepository.On("RevokeUserRefreshSession", s.ctx, userID, sessionID, logoutReason).
		Return(nil).
		Once()

	err := s.service.RevokeSession(s.ctx, userID, sessionID, sessionID)

	require.NoError(t, err)
}
