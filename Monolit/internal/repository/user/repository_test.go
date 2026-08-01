//go:build integration

package user

import (
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func testUser() models.CurrentUser {
	post := "manager"

	return models.CurrentUser{
		ID:           uuid.New(),
		Email:        "user@example.com",
		PasswordHash: "hash",
		FullName:     "Dmitry",
		FullSurname:  "Mukhachev",
		Username:     "muxa",
		Role:         models.UserRoleUser,
		Post:         &post,
		CreatedAt:    time.Now().UTC().Truncate(time.Microsecond),
	}
}

func (s *RepositorySuite) TestCreateUserAndGetByUUID() {
	input := testUser()

	created, err := s.repository.CreateUser(s.ctx, input)
	s.Require().NoError(err)
	s.Require().Equal(input.ID, created.ID)
	s.Require().Equal(input.Email, created.Email)
	s.Require().Equal(input.PasswordHash, created.PasswordHash)
	s.Require().Equal(input.FullName, created.FullName)
	s.Require().Equal(input.FullSurname, created.FullSurname)
	s.Require().Equal(input.Username, created.Username)
	s.Require().Equal(input.Role, created.Role)
	s.Require().NotNil(created.Post)
	s.Require().Equal(*input.Post, *created.Post)

	got, err := s.repository.GetUserByUUID(s.ctx, input.ID)
	s.Require().NoError(err)
	s.Require().Equal(created, got)
}

func (s *RepositorySuite) TestGetUserByEmailIsCaseInsensitive() {
	input := testUser()
	_, err := s.repository.CreateUser(s.ctx, input)
	s.Require().NoError(err)

	got, err := s.repository.GetUserByEmail(s.ctx, strings.ToUpper(input.Email))

	s.Require().NoError(err)
	s.Require().Equal(input.ID, got.ID)
	s.Require().Equal(input.Email, got.Email)
}

func (s *RepositorySuite) TestGetUserByUUIDNotFound() {
	_, err := s.repository.GetUserByUUID(s.ctx, uuid.New())

	s.Require().ErrorIs(err, models.ErrUserNotFound)
}

func (s *RepositorySuite) TestGetUserByEmailNotFound() {
	_, err := s.repository.GetUserByEmail(s.ctx, "missing@example.com")

	s.Require().ErrorIs(err, models.ErrUserNotFound)
}

func (s *RepositorySuite) TestCreateUserRejectsDuplicateEmailCaseInsensitive() {
	input := testUser()
	_, err := s.repository.CreateUser(s.ctx, input)
	s.Require().NoError(err)

	duplicate := testUser()
	duplicate.ID = uuid.New()
	duplicate.Email = strings.ToUpper(input.Email)

	_, err = s.repository.CreateUser(s.ctx, duplicate)

	s.Require().Error(err)
}

func (s *RepositorySuite) TestGetByUsernameAndUpdateUsername() {
	input := testUser()
	created, err := s.repository.CreateUser(s.ctx, input)
	s.Require().NoError(err)

	got, err := s.repository.GetUserByUsername(s.ctx, strings.ToUpper(created.Username))
	s.Require().NoError(err)
	s.Require().Equal(created.ID, got.ID)

	updated, err := s.repository.UpdateUsername(s.ctx, models.UpdateUsernameInput{
		UserUUID: created.ID,
		Username: "updated_username",
	})
	s.Require().NoError(err)
	s.Require().Equal("updated_username", updated.Username)

	got, err = s.repository.GetUserByUsername(s.ctx, "UPDATED_USERNAME")
	s.Require().NoError(err)
	s.Require().Equal(created.ID, got.ID)

	_, err = s.repository.GetUserByUsername(s.ctx, "missing")
	s.Require().ErrorIs(err, models.ErrUserNotFound)
	_, err = s.repository.UpdateUsername(s.ctx, models.UpdateUsernameInput{
		UserUUID: uuid.New(),
		Username: "missing_user",
	})
	s.Require().ErrorIs(err, models.ErrUserNotFound)
}

func (s *RepositorySuite) TestUpdateUsernameRejectsDuplicateCaseInsensitive() {
	first := testUser()
	_, err := s.repository.CreateUser(s.ctx, first)
	s.Require().NoError(err)

	second := testUser()
	second.ID = uuid.New()
	second.Email = "second@example.com"
	second.Username = "second_user"
	_, err = s.repository.CreateUser(s.ctx, second)
	s.Require().NoError(err)

	_, err = s.repository.UpdateUsername(s.ctx, models.UpdateUsernameInput{
		UserUUID: second.ID,
		Username: strings.ToUpper(first.Username),
	})
	s.Require().ErrorIs(err, models.ErrUserAlreadyExists)
}
