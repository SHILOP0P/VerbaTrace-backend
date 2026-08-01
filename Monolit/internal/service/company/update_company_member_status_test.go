package company

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestUpdateCompanyMemberStatusSuccess() {
	statuses := []models.MembershipStatus{
		models.MembershipStatusActive,
		models.MembershipStatusSuspended,
		models.MembershipStatusLeft,
	}

	for _, status := range statuses {
		s.Run(string(status), func() {
			s.SetupTest()
			companyID := uuid.New()
			managerID := uuid.New()
			userID := uuid.New()

			s.repository.EXPECT().
				GetCompanyMember(mock.Anything, companyID, managerID).
				Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
				Once()
			if status != models.MembershipStatusActive {
				s.repository.EXPECT().
					GetCompanyMember(mock.Anything, companyID, userID).
					Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
					Once()
			}
			s.repository.EXPECT().
				UpdateCompanyMemberStatus(mock.Anything, companyID, userID, status).
				Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Status: status}, nil).
				Once()

			got, err := s.service.UpdateCompanyMemberStatus(s.ctx, models.UpdateCompanyMemberStatusInput{
				CompanyUUID: companyID,
				RequestUser: managerID,
				UserUUID:    userID,
				Status:      status,
			})

			s.Require().NoError(err)
			s.Require().Equal(status, got.Status)
		})
	}
}

func (s *ServiceSuite) TestUpdateCompanyMemberStatusRejectsInvalidStatus() {
	_, err := s.service.UpdateCompanyMemberStatus(s.ctx, models.UpdateCompanyMemberStatusInput{
		CompanyUUID: uuid.New(),
		RequestUser: uuid.New(),
		UserUUID:    uuid.New(),
		Status:      models.MembershipStatus("deleted"),
	})

	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)
}

func (s *ServiceSuite) TestUpdateCompanyMemberStatusRejectsSelfUpdate() {
	userID := uuid.New()

	_, err := s.service.UpdateCompanyMemberStatus(s.ctx, models.UpdateCompanyMemberStatusInput{
		CompanyUUID: uuid.New(),
		RequestUser: userID,
		UserUUID:    userID,
		Status:      models.MembershipStatusSuspended,
	})

	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)
}

func (s *ServiceSuite) TestUpdateCompanyMemberStatusReturnsRepositoryError() {
	companyID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("update failed")

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()
	s.repository.EXPECT().
		UpdateCompanyMemberStatus(mock.Anything, companyID, userID, models.MembershipStatusSuspended).
		Return(models.CompanyMember{}, repoErr).
		Once()

	_, err := s.service.UpdateCompanyMemberStatus(s.ctx, models.UpdateCompanyMemberStatusInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
		UserUUID:    userID,
		Status:      models.MembershipStatusSuspended,
	})

	s.Require().ErrorIs(err, repoErr)
}

func (s *ServiceSuite) TestUpdateCompanyMemberStatusRejectsLastManagerRemoval() {
	companyID := uuid.New()
	managerID := uuid.New()
	targetManagerID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, targetManagerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: targetManagerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		CountActiveCompanyManagers(mock.Anything, companyID, targetManagerID).
		Return(0, nil).
		Once()

	_, err := s.service.UpdateCompanyMemberStatus(s.ctx, models.UpdateCompanyMemberStatusInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
		UserUUID:    targetManagerID,
		Status:      models.MembershipStatusSuspended,
	})

	s.Require().ErrorIs(err, models.ErrLastCompanyManager)
}
