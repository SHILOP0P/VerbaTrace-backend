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

func (s *APISuite) TestAddCompanyMemberSuccess() {
	companyID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("AddCompanyMember", mock.Anything, models.AddCompanyMemberInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
	}).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive, CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/members", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, requestUserID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.AddCompanyMember(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestAddCompanyMemberRequiresAuth() {
	companyID := uuid.New()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/members", `{}`, uuid.Nil, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.AddCompanyMember(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestAddCompanyMemberRejectsInvalidCompanyUUID() {
	rec, req := s.request(http.MethodPost, "/api/v1/companies/bad/members", `{}`, uuid.New(), map[string]string{
		"uuid": "bad",
	})

	s.api.AddCompanyMember(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestAddCompanyMemberRejectsInvalidBody() {
	companyID := uuid.New()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/members", `{`, uuid.New(), map[string]string{
		"uuid": companyID.String(),
	})

	s.api.AddCompanyMember(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRequestBody)
}

func (s *APISuite) TestAddCompanyMemberRejectsInvalidUserUUID() {
	companyID := uuid.New()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/members", `{"user_uuid":"bad","role":"employee"}`, uuid.New(), map[string]string{
		"uuid": companyID.String(),
	})

	s.api.AddCompanyMember(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestAddCompanyMemberMapsForbidden() {
	companyID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("AddCompanyMember", mock.Anything, models.AddCompanyMemberInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
	}).
		Return(models.CompanyMember{}, models.ErrForbidden).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/members", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, requestUserID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.AddCompanyMember(rec, req)

	s.Require().Equal(http.StatusForbidden, rec.Code)
	s.requireErrorCode(rec, response.CodeForbidden)
}

func (s *APISuite) TestAddCompanyMemberMapsCompanyNotFound() {
	companyID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("AddCompanyMember", mock.Anything, mock.Anything).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/members", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, requestUserID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.AddCompanyMember(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeCompanyNotFound)
}

func (s *APISuite) TestAddCompanyMemberMapsUnexpectedError() {
	companyID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("AddCompanyMember", mock.Anything, mock.Anything).
		Return(models.CompanyMember{}, errors.New("add failed")).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/members", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, requestUserID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.AddCompanyMember(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeFailedToAddCompanyMember)
}
