package company

import (
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestGetCompanyMembersOverviewSuccess() {
	companyID := uuid.New()
	userID := uuid.New()
	overview := models.CompanyMembersOverview{CompanyUUID: companyID}

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		GetCompanyMembersOverview(mock.Anything, companyID).
		Return(overview, nil).
		Once()

	got, err := s.service.GetCompanyMembersOverview(s.ctx, companyID, userID)

	s.Require().NoError(err)
	s.Require().Equal(overview, got)
}

func (s *ServiceSuite) TestGetCompanyMembersOverviewRejectsInvalidInput() {
	_, err := s.service.GetCompanyMembersOverview(s.ctx, uuid.Nil, uuid.New())
	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)

	_, err = s.service.GetCompanyMembersOverview(s.ctx, uuid.New(), uuid.Nil)
	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)
}

func (s *ServiceSuite) TestGetCompanyMembersOverviewRejectsNonManager() {
	companyID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()

	_, err := s.service.GetCompanyMembersOverview(s.ctx, companyID, userID)

	s.Require().ErrorIs(err, models.ErrForbidden)
}
