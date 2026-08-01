package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestLoginSuccess() {
	userID := uuid.New()
	body := `{"email":"user@example.com","password":"password123"}`

	s.service.On("Login", mock.Anything, mock.MatchedBy(func(input models.LoginInput) bool {
		return input.Email == "user@example.com" &&
			input.Password == "password123" &&
			input.IPAddress != nil
	})).
		Return(models.CurrentUser{ID: userID, Email: "user@example.com", FullName: "Dmitry", FullSurname: "Mukhachev", Username: "muxa", Role: models.UserRoleUser, CreatedAt: time.Now().UTC()}, "access", "refresh", nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/auth/login", body)

	s.api.Login(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	s.requireAuthCookies(rec, "access", "refresh")

	var resp dto.AuthResponse
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &resp))
	s.Require().Equal(userID.String(), resp.User.ID)
	s.Require().NotContains(rec.Body.String(), "access_token")
	s.Require().NotContains(rec.Body.String(), "refresh_token")
}

func (s *APISuite) TestLoginRejectsInvalidBody() {
	rec, req := s.request(http.MethodPost, "/api/v1/auth/login", `{`)

	s.api.Login(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRequestBody)
}

func (s *APISuite) TestLoginMapsInvalidCredentials() {
	body := `{"email":"user@example.com","password":"bad"}`

	s.service.On("Login", mock.Anything, mock.MatchedBy(func(input models.LoginInput) bool {
		return input.Email == "user@example.com" && input.Password == "bad"
	})).
		Return(models.CurrentUser{}, "", "", models.ErrInvalidCredentials).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/auth/login", body)

	s.api.Login(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCredentials)
}
