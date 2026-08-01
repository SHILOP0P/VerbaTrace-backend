package call

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestUpdateCallTitleSuccess() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateCallTitle", mock.Anything, callID, userID, "new title").
		Return(models.Call{
			ID:              callID,
			Title:           "new title",
			Status:          models.CallStatusNew,
			VisibilityScope: models.CallVisibilityScopePersonal,
			CreatedAt:       time.Now().UTC(),
		}, nil).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/calls/"+callID.String(), `{"title":"new title"}`, userID, map[string]string{
		"uuid": callID.String(),
	})

	s.api.UpdateCallTitle(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestUpdateCallTitleRequiresAuth() {
	rec, req := s.request(http.MethodPatch, "/api/v1/calls/"+uuid.New().String(), `{"title":"new title"}`, uuid.Nil, nil)

	s.api.UpdateCallTitle(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestUpdateCallTitleRejectsInvalidCallUUID() {
	rec, req := s.request(http.MethodPatch, "/api/v1/calls/bad", `{"title":"new title"}`, uuid.New(), map[string]string{
		"uuid": "bad",
	})

	s.api.UpdateCallTitle(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCallUUID)
}

func (s *APISuite) TestUpdateCallTitleMapsInvalidTitle() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateCallTitle", mock.Anything, callID, userID, "").
		Return(models.Call{}, models.ErrInvalidCallTitle).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/calls/"+callID.String(), `{"title":""}`, userID, map[string]string{
		"uuid": callID.String(),
	})

	s.api.UpdateCallTitle(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCallTitle)
}

func (s *APISuite) TestUpdateCallTitleMapsNotFound() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateCallTitle", mock.Anything, callID, userID, "new title").
		Return(models.Call{}, models.ErrCallNotFound).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/calls/"+callID.String(), `{"title":"new title"}`, userID, map[string]string{
		"uuid": callID.String(),
	})

	s.api.UpdateCallTitle(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeCallNotFound)
}
