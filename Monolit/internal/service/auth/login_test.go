package auth

import (
	"context"
	"time"

	"calllens/monolit/internal/auth/password"
	"calllens/monolit/internal/auth/token"
	"calllens/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestLoginSuccess() {
	userID := uuid.New()
	sessionID := uuid.New()
	hash, err := password.Hash("password123", "password-pepper")
	s.Require().NoError(err)

	user := models.CurrentUser{
		ID:           userID,
		Email:        "user@example.com",
		PasswordHash: hash,
		Role:         models.UserRoleUser,
	}

	s.userRepository.On("GetUserByEmail", s.ctx, "user@example.com").
		Return(user, nil).
		Once()
	s.refreshSessionRepository.On("CreateRefreshSession", s.ctx, mock.MatchedBy(func(session models.RefreshSession) bool {
		return session.UserID == userID &&
			session.AccessVersion == 1 &&
			session.RefreshTokenHash != "" &&
			session.ExpiresAt.After(time.Now().UTC())
	})).
		Return(func(_ context.Context, session models.RefreshSession) models.RefreshSession {
			session.ID = sessionID
			return session
		}, nil).
		Once()

	gotUser, accessToken, refreshToken, err := s.service.Login(s.ctx, models.LoginInput{
		Email:    " USER@example.com ",
		Password: "password123",
	})

	s.Require().NoError(err)
	s.Require().Equal(userID, gotUser.ID)
	s.Require().NotEmpty(accessToken)
	s.Require().NotEmpty(refreshToken)
	claims, err := token.ParseAccessToken(accessToken, "jwt-secret")
	s.Require().NoError(err)
	s.Require().Equal(int64(1), claims.AccessVersion)
}

func (s *ServiceSuite) TestLoginRejectsEmptyCredentials() {
	_, _, _, err := s.service.Login(s.ctx, models.LoginInput{})

	s.Require().ErrorIs(err, models.ErrInvalidCredentials)
}

func (s *ServiceSuite) TestLoginRejectsWrongPassword() {
	hash, err := password.Hash("password123", "password-pepper")
	s.Require().NoError(err)

	s.userRepository.On("GetUserByEmail", s.ctx, "user@example.com").
		Return(models.CurrentUser{ID: uuid.New(), Email: "user@example.com", PasswordHash: hash}, nil).
		Once()

	_, _, _, err = s.service.Login(s.ctx, models.LoginInput{
		Email:    "user@example.com",
		Password: "wrong-password",
	})

	s.Require().ErrorIs(err, models.ErrInvalidCredentials)
}
