package auth

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestRegisterSuccess() {
	userID := uuid.New()
	body := `{"email":"user@example.com","password":"password123","full_name":"Dmitry","full_surname":"Mukhachev","username":"muxa"}`
	input := models.CreateUserInput{
		Email:       "user@example.com",
		Password:    "password123",
		FullName:    "Dmitry",
		FullSurname: "Mukhachev",
		Username:    "muxa",
	}

	s.service.On("Register", mock.Anything, input).
		Return(models.CurrentUser{ID: userID, Email: input.Email, FullName: input.FullName, FullSurname: input.FullSurname, Username: input.Username, Role: models.UserRoleUser, CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/auth/register", body)

	s.api.Register(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestRegisterRejectsInvalidBody() {
	rec, req := s.request(http.MethodPost, "/api/v1/auth/register", `{`)

	s.api.Register(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRequestBody)
}

func (s *APISuite) TestRegisterMapsAlreadyExists() {
	body := `{"email":"user@example.com","password":"password123","full_name":"Dmitry","full_surname":"Mukhachev","username":"muxa"}`
	input := models.CreateUserInput{
		Email:       "user@example.com",
		Password:    "password123",
		FullName:    "Dmitry",
		FullSurname: "Mukhachev",
		Username:    "muxa",
	}

	s.service.On("Register", mock.Anything, input).
		Return(models.CurrentUser{}, models.ErrUserAlreadyExists).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/auth/register", body)

	s.api.Register(rec, req)

	s.Require().Equal(http.StatusConflict, rec.Code)
	s.requireErrorCode(rec, response.CodeUserAlreadyExists)
}
