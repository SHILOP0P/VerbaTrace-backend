package company

import (
	"errors"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestListSuccess() {
	userID := uuid.New()

	s.service.On("ListUserCompanies", mock.Anything, userID).
		Return([]models.Company{
			{
				ID:              uuid.New(),
				Name:            "VerbaTrace",
				ManagerUserUUID: userID,
				MemberLimit:     10,
				CreatedAt:       time.Now().UTC(),
			},
		}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies", "", userID, nil)

	s.api.List(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestListRequiresAuth() {
	rec, req := s.request(http.MethodGet, "/api/v1/companies", "", uuid.Nil, nil)

	s.api.List(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestListMapsServiceError() {
	userID := uuid.New()

	s.service.On("ListUserCompanies", mock.Anything, userID).
		Return(nil, errors.New("list failed")).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies", "", userID, nil)

	s.api.List(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeFailedToListCompanies)
}
