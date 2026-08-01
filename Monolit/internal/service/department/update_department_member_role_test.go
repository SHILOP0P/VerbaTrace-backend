package department

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestUpdateDepartmentMemberRoleSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()
	s.departmentRepository.EXPECT().
		UpdateDepartmentMemberRole(mock.Anything, companyID, departmentID, userID, models.DepartmentMemberRoleLeader).
		Return(models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: userID, Role: models.DepartmentMemberRoleLeader}, nil).
		Once()

	got, err := s.service.UpdateDepartmentMemberRole(s.ctx, models.UpdateDepartmentMemberRoleInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    managerID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleLeader,
	})

	s.Require().NoError(err)
	s.Require().Equal(models.DepartmentMemberRoleLeader, got.Role)
}

func (s *ServiceSuite) TestUpdateDepartmentMemberRoleRejectsInvalidRole() {
	_, err := s.service.UpdateDepartmentMemberRole(s.ctx, models.UpdateDepartmentMemberRoleInput{
		CompanyUUID:    uuid.New(),
		DepartmentUUID: uuid.New(),
		RequestUser:    uuid.New(),
		UserUUID:       uuid.New(),
		Role:           models.DepartmentMemberRole("manager"),
	})

	s.Require().ErrorIs(err, models.ErrInvalidDepartmentInput)
}

func (s *ServiceSuite) TestUpdateDepartmentMemberRoleRejectsNonManager() {
	companyID := uuid.New()
	requestUserID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, requestUserID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: requestUserID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()

	_, err := s.service.UpdateDepartmentMemberRole(s.ctx, models.UpdateDepartmentMemberRoleInput{
		CompanyUUID:    companyID,
		DepartmentUUID: uuid.New(),
		RequestUser:    requestUserID,
		UserUUID:       uuid.New(),
		Role:           models.DepartmentMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, models.ErrForbidden)
}

func (s *ServiceSuite) TestUpdateDepartmentMemberRoleRequiresCompanyMemberTarget() {
	companyID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("target not found")

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, repoErr).
		Once()

	_, err := s.service.UpdateDepartmentMemberRole(s.ctx, models.UpdateDepartmentMemberRoleInput{
		CompanyUUID:    companyID,
		DepartmentUUID: uuid.New(),
		RequestUser:    managerID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, repoErr)
}

func (s *ServiceSuite) TestUpdateDepartmentMemberRoleReturnsRepositoryError() {
	companyID := uuid.New()
	departmentID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("update failed")

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID}, nil).
		Once()
	s.departmentRepository.EXPECT().
		UpdateDepartmentMemberRole(mock.Anything, companyID, departmentID, userID, models.DepartmentMemberRoleEmployee).
		Return(models.DepartmentMember{}, repoErr).
		Once()

	_, err := s.service.UpdateDepartmentMemberRole(s.ctx, models.UpdateDepartmentMemberRoleInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    managerID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, repoErr)
}
