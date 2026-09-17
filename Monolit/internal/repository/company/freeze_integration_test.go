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

// A frozen company must stop importing calls it cannot process, and the
// connection has to come back exactly as it was — including a pause the owner
// made themselves, which the freeze must not undo.
func (s *RepositorySuite) TestFreezePausesIntegrationsAndActivationBringsBackOnlyThose() {
	company, manager := s.createCompanyWithManager()

	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO subscriptions (subscription_uuid, plan_uuid, type, user_uuid, status, starts_at)
		SELECT $1, plan_uuid, 'business', $2, 'active', now() - interval '1 hour' FROM plans WHERE code='business_pro'`,
		uuid.New(), manager.ID)
	s.Require().NoError(err)

	billingAccountID := uuid.New()
	_, err = s.db.ExecContext(s.ctx, `INSERT INTO billing_accounts(billing_account_uuid,owner_type,company_uuid) VALUES($1,'company',$2)`, billingAccountID, company.ID)
	s.Require().NoError(err)
	applicationID := uuid.New()
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO developer_applications (application_uuid, owner_type, company_uuid, billing_account_uuid, name, environment, status)
		VALUES ($1, 'company', $2, $3, 'freeze fixture', 'production', 'active')`, applicationID, company.ID, billingAccountID)
	s.Require().NoError(err)

	running := uuid.New()
	pausedByOwner := uuid.New()
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO integration_connections (connection_uuid, application_uuid, company_uuid, created_by_user_uuid, name, provider, status)
		VALUES ($1, $3, $4, $5, 'running', 'bitrix24', 'active'),
		       ($2, $3, $4, $5, 'paused by the owner', 'bitrix24', 'paused')`,
		running, pausedByOwner, applicationID, company.ID, manager.ID)
	s.Require().NoError(err)

	now := time.Now().UTC()
	s.Require().NoError(s.repository.FreezeCompany(s.ctx, company.ID, models.CompanyFreezeReasonDowngrade, now))

	connectionState := func(id uuid.UUID) (string, bool) {
		var status string
		var byFreeze bool
		s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT status, paused_by_freeze FROM integration_connections WHERE connection_uuid=$1`, id).Scan(&status, &byFreeze))
		return status, byFreeze
	}

	status, byFreeze := connectionState(running)
	s.Require().Equal("paused", status)
	s.Require().True(byFreeze)

	status, byFreeze = connectionState(pausedByOwner)
	s.Require().Equal("paused", status)
	s.Require().False(byFreeze, "a pause the owner made is not the freeze's to claim")

	s.Require().NoError(s.repository.ActivateCompany(s.ctx, company.ID, now))

	status, byFreeze = connectionState(running)
	s.Require().Equal("active", status)
	s.Require().False(byFreeze)

	status, _ = connectionState(pausedByOwner)
	s.Require().Equal("paused", status, "switching the company on must not undo the owner's own pause")
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
