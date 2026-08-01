package department

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestCreateDepartmentSuccess() {
	companyID := uuid.New()
	userID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.departmentRepository.EXPECT().
		CreateDepartment(mock.Anything, mock.MatchedBy(func(department models.Department) bool {
			return department.CompanyUUID == companyID && department.Name == "Sales"
		})).
		Return(models.Department{CompanyUUID: companyID, Name: "Sales"}, nil).
		Once()

	got, err := s.service.CreateDepartment(s.ctx, models.CreateDepartmentInput{
		CompanyUUID: companyID,
		UserID:      userID,
		Name:        "  Sales  ",
	})

	s.Require().NoError(err)
	s.Require().Equal("Sales", got.Name)
	s.Require().Equal(companyID, got.CompanyUUID)
}

func (s *ServiceSuite) TestCreateDepartmentRejectsInvalidInput() {
	_, err := s.service.CreateDepartment(s.ctx, models.CreateDepartmentInput{
		CompanyUUID: uuid.New(),
		UserID:      uuid.New(),
		Name:        " ",
	})
	s.Require().ErrorIs(err, models.ErrInvalidDepartmentInput)

	_, err = s.service.CreateDepartment(s.ctx, models.CreateDepartmentInput{
		CompanyUUID: uuid.Nil,
		UserID:      uuid.New(),
		Name:        "Sales",
	})
	s.Require().ErrorIs(err, models.ErrInvalidDepartmentInput)
}

func (s *ServiceSuite) TestCreateDepartmentRejectsNonManager() {
	companyID := uuid.New()
	userID := uuid.New()

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()

	_, err := s.service.CreateDepartment(s.ctx, models.CreateDepartmentInput{
		CompanyUUID: companyID,
		UserID:      userID,
		Name:        "Sales",
	})

	s.Require().ErrorIs(err, models.ErrForbidden)
}

func (s *ServiceSuite) TestCreateDepartmentReturnsRepositoryError() {
	companyID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("create failed")

	s.companyRepository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.departmentRepository.EXPECT().
		CreateDepartment(mock.Anything, mock.Anything).
		Return(models.Department{}, repoErr).
		Once()

	_, err := s.service.CreateDepartment(s.ctx, models.CreateDepartmentInput{
		CompanyUUID: companyID,
		UserID:      userID,
		Name:        "Sales",
	})

	s.Require().ErrorIs(err, repoErr)
}
