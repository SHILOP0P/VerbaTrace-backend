package company

import (
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestAddCompanyMemberSuccess() {
	companyID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		AddCompanyMember(mock.Anything, mock.MatchedBy(func(member models.CompanyMember) bool {
			return member.CompanyUUID == companyID &&
				member.UserUUID == userID &&
				member.Role == models.CompanyMemberRoleEmployee &&
				member.Status == models.MembershipStatusActive
		})).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: userID, Role: models.CompanyMemberRoleEmployee, Status: models.MembershipStatusActive}, nil).
		Once()

	got, err := s.service.AddCompanyMember(s.ctx, models.AddCompanyMemberInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
	})

	s.Require().NoError(err)
	s.Require().Equal(userID, got.UserUUID)
	s.Require().Equal(models.CompanyMemberRoleEmployee, got.Role)
}

func (s *ServiceSuite) TestAddCompanyMemberRejectsSelfAdd() {
	companyID := uuid.New()
	managerID := uuid.New()

	_, err := s.service.AddCompanyMember(s.ctx, models.AddCompanyMemberInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
		UserUUID:    managerID,
		Role:        models.CompanyMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)
}

func (s *ServiceSuite) TestAddCompanyMemberRejectsManagerRole() {
	_, err := s.service.AddCompanyMember(s.ctx, models.AddCompanyMemberInput{
		CompanyUUID: uuid.New(),
		RequestUser: uuid.New(),
		UserUUID:    uuid.New(),
		Role:        models.CompanyMemberRoleManager,
	})

	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)
}

func (s *ServiceSuite) TestAddCompanyMemberRejectsNonManager() {
	companyID := uuid.New()
	requestUserID := uuid.New()

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, requestUserID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: requestUserID, Role: models.CompanyMemberRoleEmployee}, nil).
		Once()

	_, err := s.service.AddCompanyMember(s.ctx, models.AddCompanyMemberInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    uuid.New(),
		Role:        models.CompanyMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, models.ErrForbidden)
}

func (s *ServiceSuite) TestAddCompanyMemberReturnsRequestMemberLookupError() {
	companyID := uuid.New()
	requestUserID := uuid.New()
	repoErr := errors.New("db failed")

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, requestUserID).
		Return(models.CompanyMember{}, repoErr).
		Once()

	_, err := s.service.AddCompanyMember(s.ctx, models.AddCompanyMemberInput{
		CompanyUUID: companyID,
		RequestUser: requestUserID,
		UserUUID:    uuid.New(),
		Role:        models.CompanyMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, repoErr)
}

func (s *ServiceSuite) TestAddCompanyMemberReturnsRepositoryCreateError() {
	companyID := uuid.New()
	managerID := uuid.New()
	userID := uuid.New()
	repoErr := errors.New("create member failed")

	s.repository.EXPECT().
		GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{CompanyUUID: companyID, UserUUID: managerID, Role: models.CompanyMemberRoleManager}, nil).
		Once()
	s.repository.EXPECT().
		AddCompanyMember(mock.Anything, mock.Anything).
		Return(models.CompanyMember{}, repoErr).
		Once()

	_, err := s.service.AddCompanyMember(s.ctx, models.AddCompanyMemberInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
		UserUUID:    userID,
		Role:        models.CompanyMemberRoleEmployee,
	})

	s.Require().ErrorIs(err, repoErr)
}
