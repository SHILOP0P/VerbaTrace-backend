package auth

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestLogoutSuccess() {
	sessionID := uuid.New()

	s.service.On("Logout", mock.Anything, sessionID).
		Return(nil).
		Once()

	rec, req := s.requestWithSession(http.MethodPost, "/api/v1/auth/logout", "", sessionID)

	s.api.Logout(rec, req)

	s.Require().Equal(http.StatusNoContent, rec.Code)
	s.requireClearedAuthCookies(rec)
}

func (s *APISuite) TestLogoutRequiresSession() {
	rec, req := s.request(http.MethodPost, "/api/v1/auth/logout", "")

	s.api.Logout(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestLogoutMapsSessionNotFound() {
	sessionID := uuid.New()

	s.service.On("Logout", mock.Anything, sessionID).
		Return(models.ErrRefreshSessionNotFound).
		Once()

	rec, req := s.requestWithSession(http.MethodPost, "/api/v1/auth/logout", "", sessionID)

	s.api.Logout(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeRefreshSessionNotFound)
}
