package auth

import (
	"context"

	"calllens/monolit/internal/models"

	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestRegisterSuccess() {
	input := models.CreateUserInput{
		Email:       " USER@example.com ",
		Password:    "password123",
		FullName:    " Dmitry ",
		FullSurname: " Mukhachev ",
		Username:    " muxa ",
	}

	s.userRepository.On("GetUserByEmail", s.ctx, "user@example.com").
		Return(models.CurrentUser{}, models.ErrUserNotFound).
		Once()
	s.userRepository.On("GetUserByUsername", s.ctx, "@muxa").
		Return(models.CurrentUser{}, models.ErrUserNotFound).
		Once()
	s.userRepository.On("CreateUser", s.ctx, mock.MatchedBy(func(user models.CurrentUser) bool {
		return user.Email == "user@example.com" &&
			user.FullName == "Dmitry" &&
			user.FullSurname == "Mukhachev" &&
			user.Username == "@muxa" &&
			user.Role == models.UserRoleUser &&
			user.PasswordHash != ""
	})).
		Return(func(_ context.Context, user models.CurrentUser) models.CurrentUser {
			return user
		}, nil).
		Once()

	got, err := s.service.Register(s.ctx, input)

	s.Require().NoError(err)
	s.Require().Equal("user@example.com", got.Email)
	s.Require().Equal(models.UserRoleUser, got.Role)
}

func (s *ServiceSuite) TestRegisterRejectsInvalidInput() {
	_, err := s.service.Register(s.ctx, models.CreateUserInput{
		Email:       "user@example.com",
		Password:    "short",
		FullName:    "Dmitry",
		FullSurname: "Mukhachev",
		Username:    "muxa",
	})

	s.Require().ErrorIs(err, models.ErrInvalidUserInput)
}

func (s *ServiceSuite) TestRegisterRejectsExistingUser() {
	input := models.CreateUserInput{
		Email:       "user@example.com",
		Password:    "password123",
		FullName:    "Dmitry",
		FullSurname: "Mukhachev",
		Username:    "muxa",
	}

	s.userRepository.On("GetUserByEmail", s.ctx, "user@example.com").
		Return(models.CurrentUser{Email: "user@example.com"}, nil).
		Once()

	_, err := s.service.Register(s.ctx, input)

	s.Require().ErrorIs(err, models.ErrUserAlreadyExists)
}
