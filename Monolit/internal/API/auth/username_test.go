package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestUpdateUsernameSuccess() {
	userID := uuid.New()
	s.service.EXPECT().UpdateUsername(mock.Anything, models.UpdateUsernameInput{
		UserUUID: userID, Username: "Valid Name",
	}).Return(models.CurrentUser{ID: userID, Username: "@valid_name"}, nil).Once()
	rec, req := s.requestWithUser(http.MethodPatch, "/auth/me/username", `{"username":"Valid Name"}`, userID)
	s.api.UpdateUsername(rec, req)
	s.Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestUpdateUsernameErrors() {
	tests := []struct {
		name string
		body string
		err  error
		code int
	}{
		{name: "invalid input", body: `{"username":"x"}`, err: models.ErrInvalidUserInput, code: http.StatusBadRequest},
		{name: "taken", body: `{"username":"taken"}`, err: models.ErrUserAlreadyExists, code: http.StatusConflict},
		{name: "fallback", body: `{"username":"valid"}`, err: errors.New("db"), code: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			userID := uuid.New()
			s.service.EXPECT().UpdateUsername(mock.Anything, mock.Anything).Return(models.CurrentUser{}, tt.err).Once()
			rec, req := s.requestWithUser(http.MethodPatch, "/", tt.body, userID)
			s.api.UpdateUsername(rec, req)
			s.Equal(tt.code, rec.Code)
		})
	}

	rec, req := s.request(http.MethodPatch, "/", `{}`)
	s.api.UpdateUsername(rec, req)
	s.Equal(http.StatusUnauthorized, rec.Code)

	rec, req = s.requestWithUser(http.MethodPatch, "/", `{`, uuid.New())
	s.api.UpdateUsername(rec, req)
	s.Equal(http.StatusBadRequest, rec.Code)
}

func (s *APISuite) TestLookupUser() {
	userID := uuid.New()
	s.service.EXPECT().GetUserByUsername(mock.Anything, "valid").
		Return(models.CurrentUser{ID: userID, Username: "@valid"}, nil).Once()
	rec, req := s.request(http.MethodGet, "/users/lookup?username=valid", "")
	s.api.LookupUser(rec, req)
	s.Equal(http.StatusOK, rec.Code)

	for _, tt := range []struct {
		err  error
		code int
	}{
		{models.ErrInvalidUserInput, http.StatusBadRequest},
		{models.ErrUserNotFound, http.StatusNotFound},
		{errors.New("db"), http.StatusInternalServerError},
	} {
		s.service.EXPECT().GetUserByUsername(mock.Anything, "bad").Return(models.CurrentUser{}, tt.err).Once()
		rec, req = s.request(http.MethodGet, "/users/lookup?username=bad", "")
		s.api.LookupUser(rec, req)
		s.Equal(tt.code, rec.Code)
	}
}

func TestWriteUsernameError(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code string
	}{
		{models.ErrInvalidUserInput, response.CodeInvalidUserInput},
		{models.ErrUserAlreadyExists, response.CodeUserAlreadyExists},
		{models.ErrUserNotFound, response.CodeUserNotFound},
		{errors.New("db"), response.CodeFailedToGetUser},
	} {
		rec := httptest.NewRecorder()
		writeUsernameError(rec, tt.err, response.CodeFailedToGetUser, "failed")
		var body response.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Error.Code != tt.code {
			t.Fatalf("code = %q, want %q", body.Error.Code, tt.code)
		}
	}
}
