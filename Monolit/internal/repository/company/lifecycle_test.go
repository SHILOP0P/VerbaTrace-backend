//go:build integration

package company

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// A company that stops being covered freezes, then waits out a soft deletion,
// and only then disappears. Its people stay, only their membership goes.
func (s *RepositorySuite) TestCompanyLifecycleFreezeSoftDeleteAndPurge() {
	company, manager := s.createCompanyWithManager()
	employee := s.createUser(uuid.NewString() + "@example.com")
	s.addMember(company.ID, employee.ID, models.CompanyMemberRoleEmployee, models.MembershipStatusActive)

	now := time.Now().UTC()
	s.Require().NoError(s.repository.FreezeCompany(s.ctx, company.ID, now))

	lifecycle, err := s.repository.GetCompanyLifecycle(s.ctx, company.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.CompanyLifecycleFrozen, lifecycle.State)
	s.Require().NotNil(lifecycle.PurgeAfter)

	// The freeze has not run out yet, so nothing moves.
	moved, err := s.repository.SoftDeleteExpiredFrozenCompanies(s.ctx, now)
	s.Require().NoError(err)
	s.Require().Zero(moved)

	afterFreeze := now.Add(models.CompanyFreezeGrace + time.Minute)
	moved, err = s.repository.SoftDeleteExpiredFrozenCompanies(s.ctx, afterFreeze)
	s.Require().NoError(err)
	s.Require().Equal(int64(1), moved)

	lifecycle, err = s.repository.GetCompanyLifecycle(s.ctx, company.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.CompanyLifecycleSoftDeleted, lifecycle.State)

	// A superadmin may give it one more freeze window, once.
	s.Require().NoError(s.repository.RestoreSoftDeletedCompany(s.ctx, company.ID, afterFreeze))
	s.Require().Error(s.repository.RestoreSoftDeletedCompany(s.ctx, company.ID, afterFreeze))

	moved, err = s.repository.SoftDeleteExpiredFrozenCompanies(s.ctx, afterFreeze.Add(models.CompanyFreezeGrace+time.Minute))
	s.Require().NoError(err)
	s.Require().Equal(int64(1), moved)

	purgeTime := afterFreeze.Add(2*models.CompanyFreezeGrace + time.Minute)
	companies, err := s.repository.ClaimCompaniesForPurge(s.ctx, purgeTime, 10)
	s.Require().NoError(err)
	s.Require().Contains(companies, company.ID)

	s.Require().NoError(s.repository.PurgeCompany(s.ctx, company.ID, purgeTime))

	var remaining int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM companies WHERE company_uuid=$1`, company.ID).Scan(&remaining))
	s.Require().Zero(remaining)

	// The people are still there, only the company is gone.
	for _, userID := range []string{manager.ID.String(), employee.ID.String()} {
		var users int
		s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM users WHERE user_uuid=$1`, userID).Scan(&users))
		s.Require().Equal(1, users)
	}
}
