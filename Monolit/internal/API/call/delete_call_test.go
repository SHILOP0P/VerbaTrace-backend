package call

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestDeleteCallSuccess() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("DeleteCall", mock.Anything, callID, userID).Return(nil).Once()

	rec, req := s.request(http.MethodDelete, "/api/v1/calls/"+callID.String(), "", userID, map[string]string{"uuid": callID.String()})

	s.api.DeleteCall(rec, req)

	s.Require().Equal(http.StatusNoContent, rec.Code)
}

func (s *APISuite) TestDeleteCallRequiresAuth() {
	rec, req := s.request(http.MethodDelete, "/api/v1/calls/"+uuid.New().String(), "", uuid.Nil, nil)

	s.api.DeleteCall(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestDeleteCallRejectsInvalidUUID() {
	rec, req := s.request(http.MethodDelete, "/api/v1/calls/bad", "", uuid.New(), map[string]string{"uuid": "bad"})

	s.api.DeleteCall(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCallUUID)
}

func (s *APISuite) TestDeleteCallMapsNotFound() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("DeleteCall", mock.Anything, callID, userID).Return(models.ErrCallNotFound).Once()

	rec, req := s.request(http.MethodDelete, "/api/v1/calls/"+callID.String(), "", userID, map[string]string{"uuid": callID.String()})

	s.api.DeleteCall(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeCallNotFound)
}

func (s *APISuite) TestDeleteCallMapsUnexpectedError() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("DeleteCall", mock.Anything, callID, userID).Return(errors.New("delete failed")).Once()

	rec, req := s.request(http.MethodDelete, "/api/v1/calls/"+callID.String(), "", userID, map[string]string{"uuid": callID.String()})

	s.api.DeleteCall(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeFailedToDeleteCall)
}
