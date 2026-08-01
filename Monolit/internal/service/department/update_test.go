package department

import (
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestUpdateDepartmentSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	managerID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.departmentRepository.EXPECT().
		UpdateDepartment(mock.Anything, companyID, departmentID, "Sales").
		Return(models.Department{ID: departmentID, CompanyUUID: companyID, Name: "Sales"}, nil).
		Once()

	got, err := s.service.UpdateDepartment(s.ctx, models.UpdateDepartmentInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    managerID,
		Name:           " Sales ",
	})

	s.Require().NoError(err)
	s.Require().Equal("Sales", got.Name)
}

func (s *ServiceSuite) TestDeleteDepartmentSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	managerID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.departmentRepository.EXPECT().
		ArchiveDepartment(mock.Anything, companyID, departmentID).
		Return(nil).
		Once()

	err := s.service.DeleteDepartment(s.ctx, models.DeleteDepartmentInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    managerID,
	})

	s.Require().NoError(err)
}
