package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestLogoutAllSuccess() {
	userID := uuid.New()
	sessionID := uuid.New()

	s.service.On("LogoutAll", mock.Anything, userID, sessionID).Return(nil).Once()

	rec, req := s.requestWithUserAndSession(http.MethodPost, "/api/v1/auth/logout-all", "", userID, sessionID)

	s.api.LogoutAll(rec, req)

	s.Require().Equal(http.StatusNoContent, rec.Code)
	s.requireClearedAuthCookies(rec)
}

func (s *APISuite) TestLogoutAllMapsSessionTrustCooldown() {
	userID := uuid.New()
	sessionID := uuid.New()
	availableAt := time.Now().UTC().Add(time.Hour)

	s.service.On("LogoutAll", mock.Anything, userID, sessionID).
		Return(models.SessionTrustError{AvailableAt: availableAt}).
		Once()

	rec, req := s.requestWithUserAndSession(http.MethodPost, "/api/v1/auth/logout-all", "", userID, sessionID)
	s.api.LogoutAll(rec, req)

	s.Require().Equal(http.StatusForbidden, rec.Code)
	s.requireErrorCode(rec, response.CodeSessionTrustAgeRequired)
	s.Require().NotEmpty(rec.Header().Get("Retry-After"))
	s.Require().Empty(rec.Header().Values("Set-Cookie"))

	var body response.ErrorResponse
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &body))
	details, ok := body.Error.Details.(map[string]any)
	s.Require().True(ok)
	s.Require().Equal(availableAt.Format(time.RFC3339), details["available_at"])
	s.Require().Greater(details["retry_after_seconds"].(float64), float64(0))
}

func (s *APISuite) TestLogoutAllRequiresAuth() {
	rec, req := s.request(http.MethodPost, "/api/v1/auth/logout-all", "")

	s.api.LogoutAll(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestLogoutAllMapsServiceError() {
	userID := uuid.New()
	sessionID := uuid.New()

	s.service.On("LogoutAll", mock.Anything, userID, sessionID).Return(errors.New("db failed")).Once()

	rec, req := s.requestWithUserAndSession(http.MethodPost, "/api/v1/auth/logout-all", "", userID, sessionID)

	s.api.LogoutAll(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeFailedToLogoutAll)
}

func (s *APISuite) TestLogoutAllMapsMissingCurrentSession() {
	userID := uuid.New()
	sessionID := uuid.New()

	s.service.On("LogoutAll", mock.Anything, userID, sessionID).
		Return(models.ErrRefreshSessionNotFound).
		Once()

	rec, req := s.requestWithUserAndSession(http.MethodPost, "/api/v1/auth/logout-all", "", userID, sessionID)
	s.api.LogoutAll(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeRefreshSessionNotFound)
	s.requireClearedAuthCookies(rec)
}
