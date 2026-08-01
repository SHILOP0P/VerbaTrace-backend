package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	serviceMocks "verbatrace/monolit/internal/service/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"
)

type APISuite struct {
	suite.Suite
	ctx     context.Context
	service *serviceMocks.AuthService
	api     *AuthHandler
}

func (s *APISuite) SetupTest() {
	s.ctx = context.Background()
	s.service = serviceMocks.NewAuthService(s.T())
	s.api = NewAuthHandler(s.service, 15*time.Minute, 30*24*time.Hour)
}

func TestAPISuite(t *testing.T) {
	suite.Run(t, new(APISuite))
}

func (s *APISuite) request(method string, path string, body string) (*httptest.ResponseRecorder, *http.Request) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(s.ctx)
	return httptest.NewRecorder(), req
}

func (s *APISuite) requestWithUser(method string, path string, body string, userID uuid.UUID) (*httptest.ResponseRecorder, *http.Request) {
	rec, req := s.request(method, path, body)
	req = req.WithContext(middleware.ContextWithUserID(req.Context(), userID))
	return rec, req
}

func (s *APISuite) requestWithSession(method string, path string, body string, sessionID uuid.UUID) (*httptest.ResponseRecorder, *http.Request) {
	rec, req := s.request(method, path, body)
	req = req.WithContext(middleware.ContextWithSessionID(req.Context(), sessionID))
	return rec, req
}

func (s *APISuite) requestWithUserAndSession(method string, path string, body string, userID uuid.UUID, sessionID uuid.UUID) (*httptest.ResponseRecorder, *http.Request) {
	rec, req := s.requestWithUser(method, path, body, userID)
	req = req.WithContext(middleware.ContextWithSessionID(req.Context(), sessionID))
	return rec, req
}

func (s *APISuite) requireErrorCode(rec *httptest.ResponseRecorder, expectedCode string) {
	var resp response.ErrorResponse
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &resp))
	s.Require().Equal(expectedCode, resp.Error.Code)
}

func (s *APISuite) requireAuthCookies(rec *httptest.ResponseRecorder, expectedAccessToken string, expectedRefreshToken string) {
	cookies := rec.Result().Cookies()

	accessCookie := findCookie(cookies, accessTokenCookieName)
	s.Require().NotNil(accessCookie)
	s.Require().Equal(expectedAccessToken, accessCookie.Value)
	s.Require().True(accessCookie.HttpOnly)
	s.Require().Equal(accessTokenCookiePath, accessCookie.Path)
	s.Require().Equal(http.SameSiteLaxMode, accessCookie.SameSite)

	refreshCookie := findCookie(cookies, refreshTokenCookieName)
	s.Require().NotNil(refreshCookie)
	s.Require().Equal(expectedRefreshToken, refreshCookie.Value)
	s.Require().True(refreshCookie.HttpOnly)
	s.Require().Equal(refreshTokenCookiePath, refreshCookie.Path)
	s.Require().Equal(http.SameSiteLaxMode, refreshCookie.SameSite)
}

func (s *APISuite) requireClearedAuthCookies(rec *httptest.ResponseRecorder) {
	cookies := rec.Result().Cookies()

	accessCookie := findCookie(cookies, accessTokenCookieName)
	s.Require().NotNil(accessCookie)
	s.Require().Equal("", accessCookie.Value)
	s.Require().Negative(accessCookie.MaxAge)
	s.Require().Equal(accessTokenCookiePath, accessCookie.Path)

	refreshCookie := findCookie(cookies, refreshTokenCookieName)
	s.Require().NotNil(refreshCookie)
	s.Require().Equal("", refreshCookie.Value)
	s.Require().Negative(refreshCookie.MaxAge)
	s.Require().Equal(refreshTokenCookiePath, refreshCookie.Path)
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}

	return nil
}
