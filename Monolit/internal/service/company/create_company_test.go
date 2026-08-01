package company

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestCreateCompanySuccess() {
	userID := uuid.New()

	s.repository.EXPECT().
		CreateCompany(mock.Anything, mock.MatchedBy(func(company models.Company) bool {
			return company.Name == "VerbaTrace" &&
				company.ManagerUserUUID == userID &&
				company.MemberLimit == defaultMemberLimit
		}), mock.MatchedBy(func(member models.CompanyMember) bool {
			return member.UserUUID == userID &&
				member.Role == models.CompanyMemberRoleManager &&
				member.Status == models.MembershipStatusActive
		})).
		Return(models.Company{Name: "VerbaTrace", ManagerUserUUID: userID, MemberLimit: defaultMemberLimit}, nil).
		Once()

	got, err := s.service.CreateCompany(s.ctx, models.CreateCompanyInput{
		Name:          "  VerbaTrace  ",
		ManagerUserID: userID,
	})

	s.Require().NoError(err)
	s.Require().Equal("VerbaTrace", got.Name)
	s.Require().Equal(userID, got.ManagerUserUUID)
}

func (s *ServiceSuite) TestCreateCompanyRejectsInvalidInput() {
	_, err := s.service.CreateCompany(s.ctx, models.CreateCompanyInput{
		Name:          " ",
		ManagerUserID: uuid.New(),
	})
	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)

	_, err = s.service.CreateCompany(s.ctx, models.CreateCompanyInput{
		Name:          "VerbaTrace",
		ManagerUserID: uuid.Nil,
	})
	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)
}

func (s *ServiceSuite) TestCreateCompanyReturnsCreateError() {
	userID := uuid.New()
	repoErr := errors.New("create failed")

	s.repository.EXPECT().
		CreateCompany(mock.Anything, mock.Anything, mock.Anything).
		Return(models.Company{}, repoErr).
		Once()

	_, err := s.service.CreateCompany(s.ctx, models.CreateCompanyInput{
		Name:          "VerbaTrace",
		ManagerUserID: userID,
	})

	s.Require().ErrorIs(err, repoErr)
}
