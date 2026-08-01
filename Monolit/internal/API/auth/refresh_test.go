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

func (s *APISuite) TestRefreshSuccess() {
	userID := uuid.New()

	s.service.On("Refresh", mock.Anything, models.RefreshTokenInput{RefreshToken: "refresh"}).
		Return(models.CurrentUser{ID: userID, Email: "user@example.com", FullName: "Dmitry", FullSurname: "Mukhachev", Username: "muxa", Role: models.UserRoleUser, CreatedAt: time.Now().UTC()}, "access", "new-refresh", nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/auth/refresh", "")
	req.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: "refresh", Path: refreshTokenCookiePath})

	s.api.Refresh(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	s.requireAuthCookies(rec, "access", "new-refresh")

	var resp dto.AuthResponse
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &resp))
	s.Require().Equal(userID.String(), resp.User.ID)
	s.Require().NotContains(rec.Body.String(), "access_token")
	s.Require().NotContains(rec.Body.String(), "refresh_token")
}

func (s *APISuite) TestRefreshIgnoresBodyTokenWithoutCookie() {
	s.service.On("Refresh", mock.Anything, models.RefreshTokenInput{RefreshToken: ""}).
		Return(models.CurrentUser{}, "", "", models.ErrInvalidRefreshToken).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/auth/refresh", `{"refresh_token":"body-token"}`)

	s.api.Refresh(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRefreshToken)
	s.requireClearedAuthCookies(rec)
}

func (s *APISuite) TestRefreshMapsInvalidRefreshToken() {
	s.service.On("Refresh", mock.Anything, models.RefreshTokenInput{RefreshToken: "bad"}).
		Return(models.CurrentUser{}, "", "", models.ErrInvalidRefreshToken).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/auth/refresh", "")
	req.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: "bad", Path: refreshTokenCookiePath})

	s.api.Refresh(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRefreshToken)
	s.requireClearedAuthCookies(rec)
}

func (s *APISuite) TestRefreshConflictDoesNotClearWinningCookies() {
	s.service.On("Refresh", mock.Anything, models.RefreshTokenInput{RefreshToken: "old"}).
		Return(models.CurrentUser{}, "", "", models.ErrRefreshRotationConflict).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/auth/refresh", "")
	req.AddCookie(&http.Cookie{Name: refreshTokenCookieName, Value: "old", Path: refreshTokenCookiePath})

	s.api.Refresh(rec, req)

	s.Require().Equal(http.StatusConflict, rec.Code)
	s.requireErrorCode(rec, response.CodeRefreshRotationConflict)
	s.Require().Empty(rec.Header().Values("Set-Cookie"))
}
