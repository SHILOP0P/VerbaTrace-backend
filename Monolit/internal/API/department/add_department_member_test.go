package department

import (
	"errors"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestAddDepartmentMemberSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("AddDepartmentMember", mock.Anything, models.AddDepartmentMemberInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    requestUserID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleEmployee,
	}).
		Return(models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: userID, Role: models.DepartmentMemberRoleEmployee, Status: models.MembershipStatusActive, CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, requestUserID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})

	s.api.AddDepartmentMember(rec, req)

	s.Require().Equal(http.StatusCreated, rec.Code)
}

func (s *APISuite) TestAddDepartmentMemberRequiresAuth() {
	companyID := uuid.New()
	departmentID := uuid.New()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members", `{}`, uuid.Nil, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})

	s.api.AddDepartmentMember(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestAddDepartmentMemberRejectsInvalidCompanyUUID() {
	departmentID := uuid.New()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/bad/departments/"+departmentID.String()+"/members", `{}`, uuid.New(), map[string]string{
		"uuid":            "bad",
		"department_uuid": departmentID.String(),
	})

	s.api.AddDepartmentMember(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidCompanyInput)
}

func (s *APISuite) TestAddDepartmentMemberRejectsInvalidDepartmentUUID() {
	companyID := uuid.New()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments/bad/members", `{}`, uuid.New(), map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": "bad",
	})

	s.api.AddDepartmentMember(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidDepartmentInput)
}

func (s *APISuite) TestAddDepartmentMemberRejectsInvalidBody() {
	companyID := uuid.New()
	departmentID := uuid.New()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members", `{`, uuid.New(), map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})

	s.api.AddDepartmentMember(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRequestBody)
}

func (s *APISuite) TestAddDepartmentMemberRejectsInvalidUserUUID() {
	companyID := uuid.New()
	departmentID := uuid.New()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members", `{"user_uuid":"bad","role":"employee"}`, uuid.New(), map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})

	s.api.AddDepartmentMember(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidDepartmentInput)
}

func (s *APISuite) TestAddDepartmentMemberMapsDepartmentNotFound() {
	companyID := uuid.New()
	departmentID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("AddDepartmentMember", mock.Anything, mock.Anything).
		Return(models.DepartmentMember{}, models.ErrDepartmentNotFound).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, requestUserID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})

	s.api.AddDepartmentMember(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeDepartmentNotFound)
}

func (s *APISuite) TestAddDepartmentMemberMapsForbidden() {
	companyID := uuid.New()
	departmentID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("AddDepartmentMember", mock.Anything, mock.Anything).
		Return(models.DepartmentMember{}, models.ErrForbidden).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, requestUserID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})

	s.api.AddDepartmentMember(rec, req)

	s.Require().Equal(http.StatusForbidden, rec.Code)
	s.requireErrorCode(rec, response.CodeForbidden)
}

func (s *APISuite) TestAddDepartmentMemberMapsUnexpectedError() {
	companyID := uuid.New()
	departmentID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("AddDepartmentMember", mock.Anything, mock.Anything).
		Return(models.DepartmentMember{}, errors.New("add failed")).
		Once()

	rec, req := s.request(http.MethodPost, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members", `{"user_uuid":"`+userID.String()+`","role":"employee"}`, requestUserID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})

	s.api.AddDepartmentMember(rec, req)

	s.Require().Equal(http.StatusInternalServerError, rec.Code)
	s.requireErrorCode(rec, response.CodeFailedToAddDepartmentMember)
}
