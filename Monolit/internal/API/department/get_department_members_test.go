package department

import (
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *APISuite) TestListDepartmentMembersSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()

	s.service.On("ListDepartmentMembers", mock.Anything, companyID, departmentID, userID).
		Return([]models.DepartmentMember{{DepartmentUUID: departmentID, UserUUID: userID, Role: models.DepartmentMemberRoleLeader, Status: models.MembershipStatusActive, CreatedAt: time.Now().UTC()}}, nil).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members", "", userID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})

	s.api.ListDepartmentMembers(rec, req)

	s.Require().Equal(http.StatusOK, rec.Code)
}

func (s *APISuite) TestListDepartmentMembersRejectsInvalidDepartmentUUID() {
	companyID := uuid.New()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/departments/bad/members", "", uuid.New(), map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": "bad",
	})

	s.api.ListDepartmentMembers(rec, req)

	s.Require().Equal(http.StatusBadRequest, rec.Code)
	s.requireErrorCode(rec, response.CodeInvalidDepartmentInput)
}

func (s *APISuite) TestListDepartmentMembersMapsForbidden() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()

	s.service.On("ListDepartmentMembers", mock.Anything, companyID, departmentID, userID).
		Return(nil, models.ErrForbidden).
		Once()

	rec, req := s.request(http.MethodGet, "/api/v1/companies/"+companyID.String()+"/departments/"+departmentID.String()+"/members", "", userID, map[string]string{
		"uuid":            companyID.String(),
		"department_uuid": departmentID.String(),
	})

	s.api.ListDepartmentMembers(rec, req)

	s.Require().Equal(http.StatusForbidden, rec.Code)
	s.requireErrorCode(rec, response.CodeForbidden)
}
