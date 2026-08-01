package department

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestUpdateDepartmentMemberStatusSuccess() {
	companyID := uuid.New()
	departmentID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.departmentRepository.EXPECT().
		UpdateDepartmentMemberStatus(mock.Anything, companyID, departmentID, userID, models.MembershipStatusSuspended).
		Return(models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: userID, Status: models.MembershipStatusSuspended}, nil).
		Once()

	got, err := s.service.UpdateDepartmentMemberStatus(s.ctx, models.UpdateDepartmentMemberStatusInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    managerID,
		UserUUID:       userID,
		Status:         models.MembershipStatusSuspended,
	})

	s.Require().NoError(err)
	s.Require().Equal(models.MembershipStatusSuspended, got.Status)
}

func (s *ServiceSuite) TestUpdateDepartmentMemberStatusRejectsInvalidStatus() {
	_, err := s.service.UpdateDepartmentMemberStatus(s.ctx, models.UpdateDepartmentMemberStatusInput{
		CompanyUUID:    uuid.New(),
		DepartmentUUID: uuid.New(),
		RequestUser:    uuid.New(),
		UserUUID:       uuid.New(),
		Status:         models.MembershipStatus("deleted"),
	})

	s.Require().ErrorIs(err, models.ErrInvalidDepartmentInput)
}

func (s *ServiceSuite) TestUpdateDepartmentMemberStatusRejectsNonManager() {
	companyID := uuid.New()
	requestUserID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, requestUserID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: requestUserID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()

	_, err := s.service.UpdateDepartmentMemberStatus(s.ctx, models.UpdateDepartmentMemberStatusInput{
		CompanyUUID:    companyID,
		DepartmentUUID: uuid.New(),
		RequestUser:    requestUserID,
		UserUUID:       uuid.New(),
		Status:         models.MembershipStatusSuspended,
	})

	s.Require().ErrorIs(err, models.ErrForbidden)
}

func (s *ServiceSuite) TestUpdateDepartmentMemberStatusReturnsRepositoryError() {
	companyID := uuid.New()
	departmentID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("update failed")

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.departmentRepository.EXPECT().
		UpdateDepartmentMemberStatus(mock.Anything, companyID, departmentID, userID, models.MembershipStatusSuspended).
		Return(models.DepartmentMember{}, repoErr).
		Once()

	_, err := s.service.UpdateDepartmentMemberStatus(s.ctx, models.UpdateDepartmentMemberStatusInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    managerID,
		UserUUID:       userID,
		Status:         models.MembershipStatusSuspended,
	})

	s.Require().ErrorIs(err, repoErr)
}
