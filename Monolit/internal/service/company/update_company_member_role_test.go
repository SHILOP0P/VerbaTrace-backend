package company

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestUpdateCompanyMemberRoleSuccess() {
	companyID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		UpdateCompanyMemberRole(mock.Anything, companyID, userID, models.CompanyMemberRoleEmployee).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()

	got, err := s.service.UpdateCompanyMemberRole(s.ctx, models.UpdateCompanyMemberRoleInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
	})

	s.Require().NoError(err)
	s.Require().Equal(models.CompanyMemberRoleEmployee, got.Role)
}

func (s *ServiceSuite) TestUpdateCompanyMemberRoleRejectsSelfUpdate() {
	userID := uuid.New()

	_, err := s.service.UpdateCompanyMemberRole(s.ctx, models.UpdateCompanyMemberRoleInput{
		CompanyUUID: uuid.New(),
		RequestUser: userID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)
}

func (s *ServiceSuite) TestUpdateCompanyMemberRoleRejectsManagerRole() {
	_, err := s.service.UpdateCompanyMemberRole(s.ctx, models.UpdateCompanyMemberRoleInput{
		CompanyUUID: uuid.New(),
		RequestUser: uuid.New(),
		UserUUID:    uuid.New(),
		Role:        models.CompanyMemberRoleManager,
	})

	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)
}

func (s *ServiceSuite) TestUpdateCompanyMemberRoleRejectsNonManager() {
	companyID := uuid.New()
	requestUserID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, requestUserID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: requestUserID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()

	_, err := s.service.UpdateCompanyMemberRole(s.ctx, models.UpdateCompanyMemberRoleInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    uuid.New(),
		Role:        models.CompanyMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, models.ErrForbidden)
}

func (s *ServiceSuite) TestUpdateCompanyMemberRoleReturnsRepositoryError() {
	companyID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("update failed")

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		UpdateCompanyMemberRole(mock.Anything, companyID, userID, models.CompanyMemberRoleEmployee).
		Return(models.CompanyMember{}, repoErr).
		Once()

	_, err := s.service.UpdateCompanyMemberRole(s.ctx, models.UpdateCompanyMemberRoleInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, repoErr)
}
