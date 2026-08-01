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

func (s *APISuite) TestGetByUUIDSuccess() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.On("GetCompanyByUUID", mock.Anything, companyID, userID).
		Return(models.Company{
			ID:              companyID,
			Name:            "VerbaTrace",
			ManagerUserUUID: userID,
			MemberLimit:     10,
			CreatedAt:       time.Now().UTC(),
		}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String(), "", userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.GetByUUID(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestGetByUUIDRequiresAuth() {
	companyID := uuid.New()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String(), "", uuid.Nil, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.GetByUUID(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestGetByUUIDRejectsInvalidCompanyUUID() {
	rec, req := s.request(http.MethodGet, "/api/v1/companies/bad", "", uuid.New(), map[string]string{
		"uuid": "bad",
	})

	s.api.GetByUUID(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestGetByUUIDMapsNotFound() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.On("GetCompanyByUUID", mock.Anything, companyID, userID).
		Return(models.Company{}, models.ErrCompanyNotFound).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String(), "", userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.GetByUUID(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeCompanyNotFound)
}

func (s *APISuite) TestGetByUUIDMapsServiceError() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.On("GetCompanyByUUID", mock.Anything, companyID, userID).
		Return(models.Company{}, errors.New("get company failed")).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String(), "", userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.GetByUUID(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeFailedToGetCompany)
}
