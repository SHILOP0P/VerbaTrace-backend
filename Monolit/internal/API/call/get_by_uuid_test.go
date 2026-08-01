package call

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestGetByUUIDSuccess() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("GetByUUID", mock.Anything, callID, userID).
		Return(models.Call{ID: callID, Title: "call", Status: models.CallStatusNew, VisibilityScope: models.CallVisibilityScopePersonal, CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/"+callID.String(), "", userID, map[string]string{"uuid": callID.String()})

	s.api.GetByUUID(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestGetByUUIDMapsNotFound() {
	callID := uuid.New()
	userID := uuid.New()

	s.service.On("GetByUUID", mock.Anything, callID, userID).Return(models.Call{}, models.ErrCallNotFound).Once()

	rec, req := s.request(http.MethodGet, "/api/v1/calls/"+callID.String(), "", userID, map[string]string{"uuid": callID.String()})

	s.api.GetByUUID(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeCallNotFound)
}
