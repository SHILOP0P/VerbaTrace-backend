//go:build integration

package billing

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// A company limit caps what the shared pot may be spent on, and a forecast
// projects the pace of the period that has already passed.
func (s *RepositorySuite) TestCompanyCreditLimitAndForecast() {
	ownerID := s.createUser("credit-limit-owner@example.com")
	companyID := s.createCompany(ownerID)

	limit := int64(1_000)
	s.Require().NoError(s.repository.SetCompanyCreditLimit(s.ctx, models.SetCreditLimitInput{
		CompanyUUID: companyID, UserUUID: ownerID, LimitCredits: &limit,
	}))

	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	spending, err := s.repository.CompanyCreditSpending(s.ctx, companyID, now)
	s.Require().NoError(err)
	s.Require().NotNil(spending.LimitCredits)
	s.Require().Equal(limit, *spending.LimitCredits)
	s.Require().Zero(spending.UsedCredits)
	s.Require().Zero(spending.ForecastCredits)

	// Removing the cap leaves spending unlimited within the company.
	s.Require().NoError(s.repository.SetCompanyCreditLimit(s.ctx, models.SetCreditLimitInput{
		CompanyUUID: companyID, UserUUID: ownerID, LimitCredits: nil,
	}))
	spending, err = s.repository.CompanyCreditSpending(s.ctx, companyID, now)
	s.Require().NoError(err)
	s.Require().Nil(spending.LimitCredits)
}

func (s *RepositorySuite) TestDepartmentCreditLimitIsReportedPerDepartment() {
	ownerID := s.createUser("department-limit-owner@example.com")
	companyID := s.createCompany(ownerID)

	departmentID := uuid.New()
	_, err := s.db.ExecContext(s.ctx, `INSERT INTO departments (department_uuid, company_uuid, name) VALUES ($1,$2,'Sales')`, departmentID, companyID)
	s.Require().NoError(err)

	forbidden := int64(0)
	s.Require().NoError(s.repository.SetDepartmentCreditLimit(s.ctx, models.SetCreditLimitInput{
		CompanyUUID:    companyID,
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
		UserUUID:       ownerID,
		LimitCredits:   &forbidden,
	}))

	departments, err := s.repository.DepartmentCreditSpending(s.ctx, companyID, time.Now().UTC())
	s.Require().NoError(err)
	s.Require().Len(departments, 1)
	s.Require().Equal(departmentID, departments[0].SubjectUUID)
	s.Require().NotNil(departments[0].LimitCredits)
	s.Require().Zero(*departments[0].LimitCredits)
}
