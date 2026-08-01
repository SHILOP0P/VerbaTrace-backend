package department

import (
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestListDepartmentMembersAllowsCompanyManager() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()
	members := []models.DepartmentMember{{DepartmentUUID: departmentID, UserUUID: userID}}

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.departmentRepository.EXPECT().
		ListDepartmentMembers(mock.Anything, companyID, departmentID).
		Return(members, nil).
		Once()

	got, err := s.service.ListDepartmentMembers(s.ctx, companyID, departmentID, userID)

	s.Require().NoError(err)
	s.Require().Equal(members, got)
}

func (s *ServiceSuite) TestListDepartmentMembersAllowsDepartmentLeader() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()
	members := []models.DepartmentMember{{DepartmentUUID: departmentID, UserUUID: userID}}

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, models.ErrForbidden).
		Once()
	s.departmentRepository.EXPECT().
		GetDepartmentMember(mock.Anything, companyID, departmentID, userID).
		Return(models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: userID, Role: models.DepartmentMemberRoleLeader}, nil).
		Once()
	s.departmentRepository.EXPECT().
		ListDepartmentMembers(mock.Anything, companyID, departmentID).
		Return(members, nil).
		Once()

	got, err := s.service.ListDepartmentMembers(s.ctx, companyID, departmentID, userID)

	s.Require().NoError(err)
	s.Require().Equal(members, got)
}

func (s *ServiceSuite) TestListDepartmentMembersRejectsDepartmentEmployee() {
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, models.ErrForbidden).
		Once()
	s.departmentRepository.EXPECT().
		GetDepartmentMember(mock.Anything, companyID, departmentID, userID).
		Return(models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: userID, Role: models.DepartmentMemberRoleEmployee}, nil).
		Once()

	_, err := s.service.ListDepartmentMembers(s.ctx, companyID, departmentID, userID)

	s.Require().ErrorIs(err, models.ErrForbidden)
}

func (s *ServiceSuite) TestListDepartmentMembersRejectsInvalidInput() {
	_, err := s.service.ListDepartmentMembers(s.ctx, uuid.Nil, uuid.New(), uuid.New())
	s.Require().ErrorIs(err, models.ErrInvalidDepartmentInput)

	_, err = s.service.ListDepartmentMembers(s.ctx, uuid.New(), uuid.Nil, uuid.New())
	s.Require().ErrorIs(err, models.ErrInvalidDepartmentInput)

	_, err = s.service.ListDepartmentMembers(s.ctx, uuid.New(), uuid.New(), uuid.Nil)
	s.Require().ErrorIs(err, models.ErrInvalidDepartmentInput)
}
