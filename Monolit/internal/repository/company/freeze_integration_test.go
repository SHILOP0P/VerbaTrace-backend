//go:build integration

package company

import (
	"time"

	"verbatrace/monolit/internal/companystate"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// Freezing a company stops it from taking anybody new on, so an invitation that
// would fail the moment it was accepted is cancelled instead of left hanging.
func (s *RepositorySuite) TestFreezeCancelsPendingInvitations() {
	company, manager := s.createCompanyWithManager()
	invited := s.createUser(uuid.NewString() + "@example.com")

	invitationID := uuid.New()
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO membership_invitations (invitation_uuid, company_uuid, invited_user_uuid, invited_by_user_uuid, company_role, status, expires_at)
		VALUES ($1, $2, $3, $4, 'employee', 'pending', now() + interval '7 days')`,
		invitationID, company.ID, invited.ID, manager.ID)
	s.Require().NoError(err)

	s.Require().NoError(s.repository.FreezeCompany(s.ctx, company.ID, models.CompanyFreezeReasonDowngrade, time.Now().UTC()))

	var status string
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT status FROM membership_invitations WHERE invitation_uuid=$1`, invitationID).Scan(&status))
	s.Require().Equal("canceled", status)
}

// A frozen company stays fully readable and refuses every change. The guard used
// to exist only by accident, wherever the code happened to ask for a
// subscription, so everything that did not ask kept working.
func (s *RepositorySuite) TestFrozenCompanyRefusesChangesButStaysReadable() {
	company, _ := s.createCompanyWithManager()

	s.Require().NoError(companystate.EnsureActive(s.ctx, s.db, company.ID))

	s.Require().NoError(s.repository.FreezeCompany(s.ctx, company.ID, models.CompanyFreezeReasonDowngrade, time.Now().UTC()))
	s.Require().ErrorIs(companystate.EnsureActive(s.ctx, s.db, company.ID), models.ErrCompanyFrozen)

	// Reading the company itself keeps working while it is frozen.
	lifecycle, err := s.repository.GetCompanyLifecycle(s.ctx, company.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.CompanyLifecycleFrozen, lifecycle.State)
	s.Require().Equal(models.CompanyFreezeReasonDowngrade, lifecycle.FreezeReason)
}

// Deleting a company and freezing one after a downgrade end in the same state,
// but only the downgrade is undone by switching the company back on.
func (s *RepositorySuite) TestDeletedCompanyCannotBeActivatedWithoutCancellingTheDeletion() {
	company, manager := s.createCompanyWithManager()

	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO subscriptions (subscription_uuid, plan_uuid, type, user_uuid, status, starts_at)
		SELECT $1, plan_uuid, 'business', $2, 'active', now() - interval '1 hour' FROM plans WHERE code='business_pro'`,
		uuid.New(), manager.ID)
	s.Require().NoError(err)

	now := time.Now().UTC()
	s.Require().NoError(s.repository.FreezeCompany(s.ctx, company.ID, models.CompanyFreezeReasonDeletion, now))

	s.Require().ErrorIs(s.repository.ActivateCompany(s.ctx, company.ID, now), models.ErrCompanyDeletionInProgress)

	s.Require().NoError(s.repository.CancelCompanyDeletion(s.ctx, company.ID, now))

	lifecycle, err := s.repository.GetCompanyLifecycle(s.ctx, company.ID)
	s.Require().NoError(err)
	s.Require().Equal(models.CompanyLifecycleFrozen, lifecycle.State)
	s.Require().Equal(models.CompanyFreezeReasonDowngrade, lifecycle.FreezeReason)

	// Only now does the ordinary activation apply.
	s.Require().NoError(s.repository.ActivateCompany(s.ctx, company.ID, now))
}
