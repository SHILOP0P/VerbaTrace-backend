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

// A company with calls cannot be purged until the retention worker has removed
// them together with their files. The purge used to try anyway and fail on a
// foreign key every hour, forever, without anybody noticing.
func (s *RepositorySuite) TestPurgeWaitsForTheRetentionWorkerToRemoveCalls() {
	company, manager := s.createCompanyWithManager()

	callID := uuid.New()
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO calls (call_uuid, title, status, audio_path, original_filename, mime_type, size_bytes, duration_seconds, uploaded_by_user_uuid, company_uuid, visibility_scope, created_at)
		VALUES ($1, 'call', 'analyzed', 'audio/x.ogg', 'x.ogg', 'audio/ogg', 10, 1, $2, $3, 'company', now())`,
		callID, manager.ID, company.ID)
	s.Require().NoError(err)

	now := time.Now().UTC()
	s.Require().ErrorIs(s.repository.PurgeCompany(s.ctx, company.ID, now), models.ErrCompanyPurgePending)

	var stillThere int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM companies WHERE company_uuid=$1`, company.ID).Scan(&stillThere))
	s.Require().Equal(1, stillThere)

	// Once the call is gone the purge goes through.
	_, err = s.db.ExecContext(s.ctx, `DELETE FROM calls WHERE call_uuid=$1`, callID)
	s.Require().NoError(err)
	s.Require().NoError(s.repository.PurgeCompany(s.ctx, company.ID, now))

	var remaining int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM companies WHERE company_uuid=$1`, company.ID).Scan(&remaining))
	s.Require().Zero(remaining)
}
