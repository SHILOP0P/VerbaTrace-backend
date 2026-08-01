package auth

import (
	"errors"
	"time"

	"verbatrace/monolit/internal/auth/refresh"
	"verbatrace/monolit/internal/auth/token"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestRefreshSuccess() {
	userID := uuid.New()
	sessionID := uuid.New()
	rawRefreshToken := "refresh-token"
	oldHash, err := refresh.Hash(rawRefreshToken, "refresh-secret")
	s.Require().NoError(err)

	currentSession := models.RefreshSession{
		ID:               sessionID,
		UserID:           userID,
		RefreshTokenHash: oldHash,
		AccessVersion:    4,
		ExpiresAt:        time.Now().UTC().Add(time.Hour),
	}

	s.refreshSessionRepository.On("GetRefreshSessionByHash", s.ctx, oldHash).
		Return(currentSession, nil).
		Once()
	rotatedSession := currentSession
	rotatedSession.RefreshTokenHash = "rotated_hash"

	s.refreshSessionRepository.On("RotateRefreshSession", s.ctx, oldHash, mock.MatchedBy(func(newHash string) bool {
		return newHash != "" && newHash != oldHash
	}), mock.MatchedBy(func(expiresAt time.Time) bool {
		return expiresAt.After(time.Now().UTC())
	})).
		Return(rotatedSession, nil).
		Once()
	s.userRepository.On("GetUserByUUID", s.ctx, userID).
		Return(models.CurrentUser{ID: userID, Email: "user@example.com", Role: models.UserRoleUser}, nil).
		Once()

	user, accessToken, newRefreshToken, err := s.service.Refresh(s.ctx, models.RefreshTokenInput{
		RefreshToken: rawRefreshToken,
	})

	s.Require().NoError(err)
	s.Require().Equal(userID, user.ID)
	s.Require().NotEmpty(accessToken)
	s.Require().NotEmpty(newRefreshToken)
	s.Require().NotEqual(rawRefreshToken, newRefreshToken)
	claims, err := token.ParseAccessToken(accessToken, "jwt-secret")
	s.Require().NoError(err)
	s.Require().Equal(int64(4), claims.AccessVersion)
	s.Require().Equal(string(models.UserRoleUser), claims.Role)
}

func (s *ServiceSuite) TestRefreshRejectsEmptyToken() {
	_, _, _, err := s.service.Refresh(s.ctx, models.RefreshTokenInput{RefreshToken: " "})

	s.Require().ErrorIs(err, models.ErrInvalidRefreshToken)
}

func (s *ServiceSuite) TestRefreshMapsSessionNotFoundToInvalidToken() {
	rawRefreshToken := "refresh-token"
	oldHash, err := refresh.Hash(rawRefreshToken, "refresh-secret")
	s.Require().NoError(err)

	s.refreshSessionRepository.On("GetRefreshSessionByHash", s.ctx, oldHash).
		Return(models.RefreshSession{}, models.ErrRefreshSessionNotFound).
		Once()

	_, _, _, err = s.service.Refresh(s.ctx, models.RefreshTokenInput{RefreshToken: rawRefreshToken})

	s.Require().ErrorIs(err, models.ErrInvalidRefreshToken)
}

func (s *ServiceSuite) TestRefreshRejectsExpiredOrRevokedSession() {
	rawRefreshToken := "refresh-token"
	oldHash, err := refresh.Hash(rawRefreshToken, "refresh-secret")
	s.Require().NoError(err)

	revokedAt := time.Now().UTC()
	tests := []models.RefreshSession{
		{ID: uuid.New(), UserID: uuid.New(), ExpiresAt: time.Now().UTC().Add(-time.Minute)},
		{ID: uuid.New(), UserID: uuid.New(), ExpiresAt: time.Now().UTC().Add(time.Hour), RevokedAt: &revokedAt},
	}

	for _, session := range tests {
		s.Run(session.ID.String(), func() {
			s.SetupTest()
			s.refreshSessionRepository.On("GetRefreshSessionByHash", s.ctx, oldHash).
				Return(session, nil).
				Once()

			_, _, _, err := s.service.Refresh(s.ctx, models.RefreshTokenInput{RefreshToken: rawRefreshToken})

			s.Require().ErrorIs(err, models.ErrInvalidRefreshToken)
		})
	}
}

func (s *ServiceSuite) TestRefreshMapsRotateNotFoundToInvalidToken() {
	rawRefreshToken := "refresh-token"
	oldHash, err := refresh.Hash(rawRefreshToken, "refresh-secret")
	s.Require().NoError(err)
	session := models.RefreshSession{ID: uuid.New(), UserID: uuid.New(), ExpiresAt: time.Now().UTC().Add(time.Hour)}

	s.refreshSessionRepository.On("GetRefreshSessionByHash", s.ctx, oldHash).Return(session, nil).Once()
	s.refreshSessionRepository.On("RotateRefreshSession", s.ctx, oldHash, mock.Anything, mock.Anything).
		Return(models.RefreshSession{}, models.ErrRefreshSessionNotFound).
		Once()

	_, _, _, err = s.service.Refresh(s.ctx, models.RefreshTokenInput{RefreshToken: rawRefreshToken})

	s.Require().ErrorIs(err, models.ErrInvalidRefreshToken)
}

func (s *ServiceSuite) TestRefreshReturnsRepositoryError() {
	rawRefreshToken := "refresh-token"
	oldHash, err := refresh.Hash(rawRefreshToken, "refresh-secret")
	s.Require().NoError(err)
	repoErr := errors.New("db failed")

	s.refreshSessionRepository.On("GetRefreshSessionByHash", s.ctx, oldHash).
		Return(models.RefreshSession{}, repoErr).
		Once()

	_, _, _, err = s.service.Refresh(s.ctx, models.RefreshTokenInput{RefreshToken: rawRefreshToken})

	s.Require().ErrorIs(err, repoErr)
}
