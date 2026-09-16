package department

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestUpdateDepartmentMemberStatusSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateDepartmentMemberStatus", mock.Anything, models.UpdateDepartmentMemberStatusInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    requestUserID,
		UserUUID:       userID,
		Status:         models.MembershipStatusLeft,
	}).
		Return(models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: userID, Status: models.MembershipStatusLeft, CreatedAt: time.Now().UTC()}, nil).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members/"+userID.String()+"/status", `{"status":"left"}`, requestUserID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberStatus(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestUpdateDepartmentMemberStatusRejectsInvalidBody() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members/"+userID.String()+"/status", `{`, uuid.New(), map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberStatus(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidRequestBody)
}

func (s *APISuite) TestUpdateDepartmentMemberStatusRequiresAuth() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members/"+userID.String()+"/status", `{"status":"left"}`, uuid.Nil, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberStatus(rec, req)

	s.Require().Equal(http.StatusUnauthorized, rec.Code)
	s.requireErrorCode(rec, response.CodeUnauthorized)
}

func (s *APISuite) TestUpdateDepartmentMemberStatusRejectsInvalidDepartmentUUID() {
	companyID := uuid.New()
	userID := uuid.New()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/bad/members/"+userID.String()+"/status", `{"status":"left"}`, uuid.New(), map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": "bad",
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberStatus(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidDepartmentInput)
}

func (s *APISuite) TestUpdateDepartmentMemberStatusMapsDepartmentNotFound() {
	companyID := uuid.New()
	departmentID := uuid.New()
	requestUserID := uuid.New()
	userID := uuid.New()

	s.service.On("UpdateDepartmentMemberStatus", mock.Anything, mock.Anything).
		Return(models.DepartmentMember{}, models.ErrDepartmentNotFound).
		Once()

	rec, req := s.request(http.MethodPatch, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members/"+userID.String()+"/status", `{"status":"left"}`, requestUserID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
		"user_uuid":       userID.String(),
	})

	s.api.UpdateDepartmentMemberStatus(rec, req)

	s.Require().Equal(http.StatusNotFound, rec.Code)
	s.requireErrorCode(rec, response.CodeDepartmentNotFound)
}
