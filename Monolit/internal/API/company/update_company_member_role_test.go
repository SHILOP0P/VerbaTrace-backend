package company

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestUpdateCompanyMemberRoleSuccess() {
	companyID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateCompanyMemberRole", mock.Anything, models.UpdateCompanyMemberRoleInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
	}).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive, CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/"+userID.String()+"/role", `{"role":"employee"}`, requestUserID, map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": userID.String(),
	})

	s.api.UpdateCompanyMemberRole(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestUpdateCompanyMemberRoleMapsInvalidInput() {
	companyID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateCompanyMemberRole", mock.Anything, mock.Anything).
		Return(models.CompanyMember{}, models.ErrInvalidCompanyInput).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/"+userID.String()+"/role", `{"role":"company_manager"}`, requestUserID, map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": userID.String(),
	})

	s.api.UpdateCompanyMemberRole(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestUpdateCompanyMemberRoleRequiresAuth() {
	companyID := uuid.New()
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/"+userID.String()+"/role", `{"role":"employee"}`, uuid.Nil, map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": userID.String(),
	})

	s.api.UpdateCompanyMemberRole(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestUpdateCompanyMemberRoleRejectsInvalidCompanyUUID() {
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/bad/members/"+userID.String()+"/role", `{"role":"employee"}`, uuid.New(), map[string]string{
		"uuid":      "bad",
		"user_uuid": userID.String(),
	})

	s.api.UpdateCompanyMemberRole(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestUpdateCompanyMemberRoleRejectsInvalidUserUUID() {
	companyID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/bad/role", `{"role":"employee"}`, uuid.New(), map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": "bad",
	})

	s.api.UpdateCompanyMemberRole(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestUpdateCompanyMemberRoleRejectsInvalidBody() {
	companyID := uuid.New()
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/"+userID.String()+"/role", `{`, uuid.New(), map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": userID.String(),
	})

	s.api.UpdateCompanyMemberRole(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRequestBody)
}

func (s *APISuite) TestUpdateCompanyMemberRoleMapsForbidden() {
	companyID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateCompanyMemberRole", mock.Anything, mock.Anything).
		Return(models.CompanyMember{}, models.ErrForbidden).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/members/"+userID.String()+"/role", `{"role":"employee"}`, requestUserID, map[string]string{
		"uuid":      companyID.String(),
		"user_uuid": userID.String(),
	})

	s.api.UpdateCompanyMemberRole(rec, req)

	s.Require().Equal(http.StatusForbidden, rec.Code)
	s.requireErrorCode(rec, response.CodeForbidden)
}
