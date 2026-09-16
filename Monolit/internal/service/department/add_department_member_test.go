package department

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestAddDepartmentMemberSuccess() {
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
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive}, nil).
		Once()
	s.departmentRepository.EXPECT().
		MoveMemberToDepartment(mock.Anything, mock.MatchedBy(func(input models.MoveDepartmentMemberInput) bool {
			return input.CompanyUUID == companyID &&
				input.ToDepartmentUUID == departmentID &&
				input.UserUUID == userID &&
				input.Role == models.DepartmentMemberRoleEmployee
		})).
		Return(models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: userID, Role: models.DepartmentMemberRoleEmployee, Status: models.MembershipStatusActive}, nil).
		Once()

	got, err := s.service.AddDepartmentMember(s.ctx, models.AddDepartmentMemberInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    managerID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleEmployee,
	})

	s.Require().NoError(err)
	s.Require().Equal(userID, got.UserUUID)
	s.Require().Equal(models.DepartmentMemberRoleEmployee, got.Role)
}

// A department leader no longer takes people directly: that is a transfer
// request addressed to the deputy.
func (s *ServiceSuite) TestAddDepartmentMemberRejectsDepartmentLeader() {
	companyID := uuid.New()
	departmentID := uuid.New()
	leaderID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, leaderID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: leaderID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()

	_, err := s.service.AddDepartmentMember(s.ctx, models.AddDepartmentMemberInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    leaderID,
		UserUUID:       uuid.New(),
		Role:           models.DepartmentMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, models.ErrForbidden)
}

func (s *ServiceSuite) TestAddDepartmentMemberAllowsDeputy() {
	companyID := uuid.New()
	departmentID := uuid.New()
	deputyID := uuid.New()
	userID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, deputyID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: deputyID, Role: models.CompanyMemberRoleDeputy}, nil).
		Once()
	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive}, nil).
		Once()
	s.departmentRepository.EXPECT().
		MoveMemberToDepartment(mock.Anything, mock.Anything).
		Return(models.DepartmentMember{DepartmentUUID: departmentID, UserUUID: userID, Role: models.DepartmentMemberRoleEmployee}, nil).
		Once()

	got, err := s.service.AddDepartmentMember(s.ctx, models.AddDepartmentMemberInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    deputyID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleEmployee,
	})

	s.Require().NoError(err)
	s.Require().Equal(userID, got.UserUUID)
}

func (s *ServiceSuite) TestAddDepartmentMemberRejectsInvalidRole() {
	_, err := s.service.AddDepartmentMember(s.ctx, models.AddDepartmentMemberInput{
		CompanyUUID:    uuid.New(),
		DepartmentUUID: uuid.New(),
		RequestUser:    uuid.New(),
		UserUUID:       uuid.New(),
		Role:           "unknown",
	})

	s.Require().ErrorIs(err, models.ErrInvalidDepartmentInput)
}

func (s *ServiceSuite) TestAddDepartmentMemberRequiresCompanyMemberTarget() {
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
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).
		Once()

	_, err := s.service.AddDepartmentMember(s.ctx, models.AddDepartmentMemberInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    managerID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, models.ErrCompanyNotFound)
}

func (s *ServiceSuite) TestAddDepartmentMemberReturnsRepositoryError() {
	companyID := uuid.New()
	departmentID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("move failed")

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()
	s.departmentRepository.EXPECT().
		MoveMemberToDepartment(mock.Anything, mock.Anything).
		Return(models.DepartmentMember{}, repoErr).
		Once()

	_, err := s.service.AddDepartmentMember(s.ctx, models.AddDepartmentMemberInput{
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		RequestUser:    managerID,
		UserUUID:       userID,
		Role:           models.DepartmentMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, repoErr)
}
