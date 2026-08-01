package department

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestUpdateDepartmentMemberRoleSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateDepartmentMemberRole", mock.Anything, models.UpdateDepartmentMemberRoleInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    requestUserID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleLeader,
	}).
		Return(models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: userID, Role: models.DepartmentMemberRoleLeader, Status: models.MembershipStatusActive, CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members/"+userID.String()+"/role", `{"role":"department_leader"}`, requestUserID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberRole(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestUpdateDepartmentMemberRoleMapsInvalidInput() {
	companyID := uuid.New()
	departmentID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateDepartmentMemberRole", mock.Anything, mock.Anything).
		Return(models.DepartmentMember{}, models.ErrInvalidDepartmentInput).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members/"+userID.String()+"/role", `{"role":"manager"}`, requestUserID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberRole(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidDepartmentInput)
}

func (s *APISuite) TestUpdateDepartmentMemberRoleRequiresAuth() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members/"+userID.String()+"/role", `{"role":"employee"}`, uuid.Nil, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberRole(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestUpdateDepartmentMemberRoleRejectsInvalidCompanyUUID() {
	departmentID := uuid.New()
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/bad/departments/"+departmentID.String()+"/members/"+userID.String()+"/role", `{"role":"employee"}`, uuid.New(), map[string]string{
		"uuid":            "bad",
		"department_uuid": departmentID.String(),
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberRole(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestUpdateDepartmentMemberRoleRejectsInvalidUserUUID() {
	companyID := uuid.New()
	departmentID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members/bad/role", `{"role":"employee"}`, uuid.New(), map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
		"user_uuid":       "bad",
	})

	s.api.UpdateDepartmentMemberRole(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidDepartmentInput)
}

func (s *APISuite) TestUpdateDepartmentMemberRoleRejectsInvalidBody() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members/"+userID.String()+"/role", `{`, uuid.New(), map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberRole(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRequestBody)
}

func (s *APISuite) TestUpdateDepartmentMemberRoleMapsForbidden() {
	companyID := uuid.New()
	departmentID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateDepartmentMemberRole", mock.Anything, mock.Anything).
		Return(models.DepartmentMember{}, models.ErrForbidden).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members/"+userID.String()+"/role", `{"role":"employee"}`, requestUserID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberRole(rec, req)

	s.Require().Equal(http.StatusForbidden, rec.Code)
	s.requireErrorCode(rec, response.CodeForbidden)
}
