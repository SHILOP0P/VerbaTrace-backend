package company

import (
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestListUserCompaniesSuccess() {
	userID := uuid.New()
	companies := []models.Company{{ID: uuid.New(), ManagerUserUUID: userID}}

	s.repository.EXPECT().ListUserCompanies(mock.Anything, userID).Return(companies, nil).Once()

	got, err := s.service.ListUserCompanies(s.ctx, userID)

	s.Require().NoError(err)
	s.Require().Equal(companies, got)
}

func (s *ServiceSuite) TestListUserCompaniesRejectsNilUser() {
	_, err := s.service.ListUserCompanies(s.ctx, uuid.Nil)

	s.Require().ErrorIs(err, models.ErrInvalidUserInput)
}

func (s *ServiceSuite) TestGetCompanyByUUIDRejectsInvalidInput() {
	_, err := s.service.GetCompanyByUUID(s.ctx, uuid.Nil, uuid.New())
	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)

	_, err = s.service.GetCompanyByUUID(s.ctx, uuid.New(), uuid.Nil)
	s.Require().ErrorIs(err, models.ErrInvalidCompanyInput)
}
