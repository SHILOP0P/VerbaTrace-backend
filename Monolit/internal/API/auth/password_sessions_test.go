package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestUpdatePasswordSuccess() {
	userID := uuid.New()
	sessionID := uuid.New()
	updatedAt := time.Now().UTC().Truncate(time.Second)

	s.service.On("UpdatePassword", mock.Anything, models.UpdatePasswordInput{
		UserUUID:        userID,
		SessionUUID:     sessionID,
		CurrentPassword: "old-password",
		NewPassword:     "new-password",
	}).Return(models.UpdatePasswordResult{UpdatedAt: updatedAt}, nil).Once()

	rec, req := s.requestWithUser(http.MethodPatch, "/api/v1/auth/me/password", `{"current_password":"old-password","new_password":"new-password"}`, userID)
	req = req.WithContext(middleware.ContextWithSessionID(req.Context(), sessionID))

	s.api.UpdatePassword(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)

	var resp dto.UpdatePasswordResponse
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &resp))
	s.Require().Equal(updatedAt.Format(time.RFC3339), resp.UpdatedAt)
}

func (s *APISuite) TestUpdatePasswordMapsValidationErrors() {
	userID := uuid.New()
	sessionID := uuid.New()

	s.service.On("UpdatePassword", mock.Anything, models.UpdatePasswordInput{
		UserUUID:        userID,
		SessionUUID:     sessionID,
		CurrentPassword: "old-password",
		NewPassword:     "short",
	}).Return(models.UpdatePasswordResult{}, models.ErrInvalidUserInput).Once()

	rec, req := s.requestWithUser(http.MethodPatch, "/api/v1/auth/me/password", `{"current_password":"old-password","new_password":"short"}`, userID)
	req = req.WithContext(middleware.ContextWithSessionID(req.Context(), sessionID))

	s.api.UpdatePassword(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidUserInput)
}

func (s *APISuite) TestListSessionsSuccessHidesTokenHash() {
	userID := uuid.New()
	sessionID := uuid.New()
	userAgent := "Chrome on Windows"
	ipAddress := "127.0.0.1/32"
	createdAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	lastSeenAt := time.Now().UTC().Truncate(time.Second)

	s.service.On("ListSessions", mock.Anything, userID, sessionID).
		Return([]models.UserSession{
			{
				ID:         sessionID,
				Current:    true,
				UserAgent:  &userAgent,
				IPAddress:  &ipAddress,
				CreatedAt:  createdAt,
				LastSeenAt: &lastSeenAt,
			},
		}, nil).
		Once()

	rec, req := s.requestWithUser(http.MethodGet, "/api/v1/auth/me/sessions", "", userID)
	req = req.WithContext(middleware.ContextWithSessionID(req.Context(), sessionID))

	s.api.ListSessions(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
	s.Require().NotContains(rec.Body.String(), "refresh")
	s.Require().NotContains(rec.Body.String(), "token")

	var resp dto.UserSessionsResponse
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &resp))
	s.Require().Len(resp.Sessions, 1)
	s.Require().Equal(sessionID.String(), resp.Sessions[0].ID)
	s.Require().True(resp.Sessions[0].Current)
	s.Require().Equal(&userAgent, resp.Sessions[0].UserAgent)
	s.Require().Equal(&ipAddress, resp.Sessions[0].IP)
}

func (s *APISuite) TestDeleteSessionMapsForeignSessionToNotFound() {
	userID := uuid.New()
	currentSessionID := uuid.New()
	sessionID := uuid.New()

	s.service.On("RevokeSession", mock.Anything, userID, currentSessionID, sessionID).
		Return(models.ErrRefreshSessionNotFound).
		Once()

	rec, req := s.requestWithUser(http.MethodDelete, "/api/v1/auth/me/sessions/"+sessionID.String(), "", userID)
	req = req.WithContext(middleware.ContextWithSessionID(req.Context(), currentSessionID))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("session_uuid", sessionID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	s.api.DeleteSession(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeRefreshSessionNotFound)
}

func (s *APISuite) TestDeleteCurrentSessionClearsCookies() {
	userID := uuid.New()
	sessionID := uuid.New()

	s.service.On("RevokeSession", mock.Anything, userID, sessionID, sessionID).
		Return(nil).
		Once()

	rec, req := s.requestWithUser(http.MethodDelete, "/api/v1/auth/me/sessions/"+sessionID.String(), "", userID)
	req = req.WithContext(middleware.ContextWithSessionID(req.Context(), sessionID))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("session_uuid", sessionID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	s.api.DeleteSession(rec, req)

	s.Require().Equal(http.StatusNoContent, rec.Code)
	s.requireClearedAuthCookies(rec)
}

func (s *APISuite) TestDeleteOtherSessionMapsTrustCooldown() {
	userID := uuid.New()
	currentSessionID := uuid.New()
	targetSessionID := uuid.New()
	availableAt := time.Now().UTC().Add(time.Hour)

	s.service.On("RevokeSession", mock.Anything, userID, currentSessionID, targetSessionID).
		Return(models.SessionTrustError{AvailableAt: availableAt}).
		Once()

	rec, req := s.requestWithUser(http.MethodDelete, "/api/v1/auth/me/sessions/"+targetSessionID.String(), "", userID)
	req = req.WithContext(middleware.ContextWithSessionID(req.Context(), currentSessionID))
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("session_uuid", targetSessionID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))

	s.api.DeleteSession(rec, req)

	s.Require().Equal(http.StatusForbidden, rec.Code)
	s.requireErrorCode(rec, response.CodeSessionTrustAgeRequired)
	s.Require().NotEmpty(rec.Header().Get("Retry-After"))
	s.Require().Empty(rec.Header().Values("Set-Cookie"))
}
