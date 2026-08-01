package company

import (
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestUpdateCompanySuccess() {
	companyID := uuid.New()
	managerID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		UpdateCompany(mock.Anything, companyID, "New name").
		Return(models.Company{ID: companyID, Name: "New name"}, nil).
		Once()

	got, err := s.service.UpdateCompany(s.ctx, models.UpdateCompanyInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
		Name:        "  New name  ",
	})

	s.Require().NoError(err)
	s.Require().Equal("New name", got.Name)
}

func (s *ServiceSuite) TestUpdateCompanyRejectsEmptyName() {
	_, err := s.service.UpdateCompany(s.ctx, models.UpdateCompanyInput{
		CompanyUUID: uuid.New(),
		RequestUser: uuid.New(),
		Name:        " ",
	})

	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)
}

func (s *ServiceSuite) TestUpdateCompanyTagRequiresManagerAndNormalizesTag() {
	companyID := uuid.New()
	managerID := uuid.New()
	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager, Status: models.MembershipStatusActive}, nil).
		Once()
	s.repository.On("UpdateCompanyTag", mock.Anything, companyID, "@verbatrace_team").
		Return(models.Company{ID: companyID, Tag: "@verbatrace_team"}, nil).Once()

	updated, err := s.service.UpdateCompanyTag(s.ctx, models.UpdateCompanyTagInput{CompanyUUID: companyID, RequestUser: managerID, Tag: " VerbaTrace Team "})
	s.Require().NoError(err)
	s.Require().Equal("@verbatrace_team", updated.Tag)
}

func (s *ServiceSuite) TestUpdateCompanyTagAsAdminNormalizesWithoutMembershipCheck() {
	companyID := uuid.New()
	s.repository.On("UpdateCompanyTag", mock.Anything, companyID, "@verbatrace_team").
		Return(models.Company{ID: companyID, Tag: "@verbatrace_team"}, nil).Once()

	updated, err := s.service.UpdateCompanyTagAsAdmin(s.ctx, companyID, " VerbaTrace Team ")

	s.Require().NoError(err)
	s.Require().Equal("@verbatrace_team", updated.Tag)
}

func (s *ServiceSuite) TestDeleteCompanySuccess() {
	companyID := uuid.New()
	managerID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		ArchiveCompany(mock.Anything, companyID).
		Return(nil).
		Once()

	err := s.service.DeleteCompany(s.ctx, models.DeleteCompanyInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
	})

	s.Require().NoError(err)
}
