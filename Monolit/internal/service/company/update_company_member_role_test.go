package company

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestAssignCompanyDeputySuccess() {
	companyID := uuid.New()
	ownerID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, ownerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: ownerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()
	s.repository.EXPECT().
		AssignCompanyDeputy(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleDeputy}, nil).
		Once()

	got, err := s.service.UpdateCompanyMemberRole(s.ctx, models.UpdateCompanyMemberRoleInput{
		CompanyUUID: companyID,
		RequestUser: ownerID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleDeputy,
	})

	s.Require().NoError(err)
	s.Require().Equal(models.CompanyMemberRoleDeputy, got.Role)
}

func (s *ServiceSuite) TestRevokeCompanyDeputySuccess() {
	companyID := uuid.New()
	ownerID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, ownerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: ownerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleDeputy}, nil).
		Once()
	s.repository.EXPECT().
		RevokeCompanyDeputy(mock.Anything, companyID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()

	got, err := s.service.UpdateCompanyMemberRole(s.ctx, models.UpdateCompanyMemberRoleInput{
		CompanyUUID: companyID,
		RequestUser: ownerID,
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

// A deputy must never be able to appoint or demote another deputy.
func (s *ServiceSuite) TestUpdateCompanyMemberRoleRejectsDeputy() {
	companyID := uuid.New()
	deputyID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, deputyID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: deputyID, Role: models.CompanyMemberRoleDeputy}, nil).
		Once()

	_, err := s.service.UpdateCompanyMemberRole(s.ctx, models.UpdateCompanyMemberRoleInput{
		CompanyUUID: companyID,
		RequestUser: deputyID,
		UserUUID:    uuid.New(),
		Role:        models.CompanyMemberRoleDeputy,
	})

	s.Require().ErrorIs(err, models.ErrOwnerOnlyAction)
}

func (s *ServiceSuite) TestUpdateCompanyMemberRoleReturnsRepositoryError() {
	companyID := uuid.New()
	ownerID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("update failed")

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, ownerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: ownerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()
	s.repository.EXPECT().
		AssignCompanyDeputy(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, repoErr).
		Once()

	_, err := s.service.UpdateCompanyMemberRole(s.ctx, models.UpdateCompanyMemberRoleInput{
		CompanyUUID: companyID,
		RequestUser: ownerID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleDeputy,
	})

	s.Require().ErrorIs(err, repoErr)
}
