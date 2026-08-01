package department

import (
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func (s *ServiceSuite) TestListCompanyDepartmentsSuccess() {
	companyID := uuid.New()
	userID := uuid.New()
	departments := []models.Department{{ID: uuid.New(), CompanyUUID: companyID, Name: "Sales"}}

	s.departmentRepository.EXPECT().
		ListVisibleCompanyDepartments(mock.Anything, companyID, userID).
		Return(departments, nil).
		Once()

	got, err := s.service.ListCompanyDepartments(s.ctx, companyID, userID)

	s.Require().NoError(err)
	s.Require().Equal(departments, got)
}

func (s *ServiceSuite) TestListCompanyDepartmentsRejectsInvalidInput() {
	_, err := s.service.ListCompanyDepartments(s.ctx, uuid.Nil, uuid.New())
	s.Require().ErrorIs(err, models.ErrInvalidDepartmentInput)

	_, err = s.service.ListCompanyDepartments(s.ctx, uuid.New(), uuid.Nil)
	s.Require().ErrorIs(err, models.ErrInvalidDepartmentInput)
}
