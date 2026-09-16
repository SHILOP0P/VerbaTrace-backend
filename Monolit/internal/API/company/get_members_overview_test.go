package company

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestGetCompanyMembersOverviewSuccess() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.On("ListCompanyMembers", mock.Anything, models.ListCompanyMembersInput{
		CompanyUUID: companyID,
		RequestUser: userID,
		Limit:       20,
		Offset:      0,
	}).
		Return(models.CompanyMembersResult{Limit: 20}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/members", "", userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.GetCompanyMembersOverview(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestGetCompanyMembersOverviewParsesFilters() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()
	status := models.MembershipStatusLeft
	role := string(models.DepartmentMemberRoleLeader)

	s.service.On("ListCompanyMembers", mock.Anything, models.ListCompanyMembersInput{
		CompanyUUID:    companyID,
		RequestUser:    userID,
		Status:         &status,
		Role:           &role,
		DepartmentUUID: departmentID,
		Query:          "petrov",
		Limit:          10,
		Offset:         20,
	}).
		Return(models.CompanyMembersResult{Limit: 10, Offset: 20}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/members?status=left&role=department_leader&department_uuid="+departmentID.String()+"&q=petrov&limit=10&offset=20", "", userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.GetCompanyMembersOverview(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestGetCompanyMembersOverviewRequiresAuth() {
	companyID := uuid.New()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/members", "", uuid.Nil, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.GetCompanyMembersOverview(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestGetCompanyMembersOverviewRejectsInvalidCompanyUUID() {
	rec, req := s.request(http.MethodGet, "/api/v1/companies/bad/members", "", uuid.New(), map[string]string{
		"uuid": "bad",
	})

	s.api.GetCompanyMembersOverview(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestGetCompanyMembersOverviewMapsCompanyNotFound() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.On("ListCompanyMembers", mock.Anything, models.ListCompanyMembersInput{
		CompanyUUID: companyID,
		RequestUser: userID,
		Limit:       20,
		Offset:      0,
	}).
		Return(models.CompanyMembersResult{}, models.ErrCompanyNotFound).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/members", "", userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.GetCompanyMembersOverview(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeCompanyNotFound)
}

func (s *APISuite) TestGetCompanyMembersOverviewMapsForbidden() {
	companyID := uuid.New()
	userID := uuid.New()

	s.service.On("ListCompanyMembers", mock.Anything, models.ListCompanyMembersInput{
		CompanyUUID: companyID,
		RequestUser: userID,
		Limit:       20,
		Offset:      0,
	}).
		Return(models.CompanyMembersResult{}, models.ErrForbidden).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/members", "", userID, map[string]string{
		"uuid": companyID.String(),
	})

	s.api.GetCompanyMembersOverview(rec, req)

	s.Require().Equal(http.StatusForbidden, rec.Code)
	s.requireErrorCode(rec, response.CodeForbidden)
}
