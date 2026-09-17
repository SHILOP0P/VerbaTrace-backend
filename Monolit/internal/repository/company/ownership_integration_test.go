//go:build integration

package company

import (
	"database/sql"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) grantBusinessPlan(userID uuid.UUID, code models.PlanCode, endsAt time.Time) {
	var planID uuid.UUID
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT plan_uuid FROM plans WHERE code=$1`, code).Scan(&planID))
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO subscriptions (subscription_uuid, plan_uuid, type, user_uuid, company_uuid, status, starts_at, ends_at)
		VALUES ($1, $2, 'business', $3, NULL, 'active', now() - interval '1 day', $4)
	`, uuid.New(), planID, userID, endsAt)
	s.Require().NoError(err)
}

func (s *RepositorySuite) createCompanyFor(manager models.CurrentUser) models.Company {
	company := testCompany(manager.ID)
	member := testCompanyMember(company.ID, manager.ID, models.CompanyMemberRoleManager)
	created, err := s.repository.CreateCompany(s.ctx, company, member)
	s.Require().NoError(err)

	return created
}

func (s *RepositorySuite) offer(transfer models.CompanyOwnershipTransfer) models.CompanyOwnershipTransfer {
	now := time.Now().UTC().Truncate(time.Microsecond)
	transfer.ID = uuid.New()
	transfer.CreatedAt = now
	transfer.ExpiresAt = now.Add(time.Hour)
	created, err := s.repository.CreateOwnershipTransfer(s.ctx, transfer)
	s.Require().NoError(err)

	return created
}

// Handing over the only company under a plan moves the plan too, along with the
// personal plan that comes with it. Without that the company would keep a new
// owner who has nothing to pay with, which is exactly the defect this replaces.
func (s *RepositorySuite) TestAcceptOwnershipMovesTheBusinessAndPersonalPlan() {
	company, owner := s.createCompanyWithManager()
	successor := s.createUser(uuid.NewString() + "@example.com")
	endsAt := time.Now().UTC().Add(20 * 24 * time.Hour).Truncate(time.Microsecond)
	s.grantBusinessPlan(owner.ID, models.PlanCodeBusinessPro, endsAt)

	created := s.offer(models.CompanyOwnershipTransfer{
		Scope:        models.CompanyOwnershipTransferScopeCompany,
		CompanyUUID:  uuid.NullUUID{UUID: company.ID, Valid: true},
		FromUserUUID: owner.ID,
		ToUserUUID:   successor.ID,
	})

	accepted, err := s.repository.AcceptOwnershipTransfer(s.ctx, created.ID, time.Now().UTC())
	s.Require().NoError(err)
	s.Require().Equal(models.CompanyOwnershipTransferAccepted, accepted.Status)
	s.Require().Equal([]uuid.UUID{company.ID}, accepted.CompanyUUIDs)

	var manager uuid.UUID
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT manager_user_uuid FROM companies WHERE company_uuid=$1`, company.ID).Scan(&manager))
	s.Require().Equal(successor.ID, manager)

	var businessOwner uuid.UUID
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT user_uuid FROM subscriptions WHERE type='business' AND status='active'`).Scan(&businessOwner))
	s.Require().Equal(successor.ID, businessOwner)

	// The personal plan of the package follows, at the tier of the business plan.
	// Every account starts with an open-ended personal_start, and an open-ended
	// plan is never shortened — only its tier moves up.
	var personalCode string
	var personalEnds sql.NullTime
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `
		SELECT p.code, s.ends_at FROM subscriptions s JOIN plans p ON p.plan_uuid=s.plan_uuid
		WHERE s.user_uuid=$1 AND s.type='personal' AND s.status='active'
	`, successor.ID).Scan(&personalCode, &personalEnds))
	s.Require().Equal(string(models.PlanCodePersonalPro), personalCode)
	s.Require().False(personalEnds.Valid)
}

// A personal plan that does end is carried to the later of the two dates, so the
// package the owner paid for is never cut short by the one that comes with the
// business plan, and never falls short of it either.
func (s *RepositorySuite) TestAcceptOwnershipExtendsAPersonalPlanThatEnds() {
	company, owner := s.createCompanyWithManager()
	successor := s.createUser(uuid.NewString() + "@example.com")
	endsAt := time.Now().UTC().Add(20 * 24 * time.Hour).Truncate(time.Microsecond)
	s.grantBusinessPlan(owner.ID, models.PlanCodeBusinessPlus, endsAt)

	// Give the successor a personal plan that runs out before the business one.
	_, err := s.db.ExecContext(s.ctx, `
		UPDATE subscriptions SET ends_at = $2
		WHERE user_uuid = $1 AND type = 'personal' AND status = 'active'
	`, successor.ID, time.Now().UTC().Add(2*24*time.Hour))
	s.Require().NoError(err)

	created := s.offer(models.CompanyOwnershipTransfer{
		Scope:        models.CompanyOwnershipTransferScopeCompany,
		CompanyUUID:  uuid.NullUUID{UUID: company.ID, Valid: true},
		FromUserUUID: owner.ID,
		ToUserUUID:   successor.ID,
	})

	_, err = s.repository.AcceptOwnershipTransfer(s.ctx, created.ID, time.Now().UTC())
	s.Require().NoError(err)

	var personalCode string
	var personalEnds sql.NullTime
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `
		SELECT p.code, s.ends_at FROM subscriptions s JOIN plans p ON p.plan_uuid=s.plan_uuid
		WHERE s.user_uuid=$1 AND s.type='personal' AND s.status='active'
	`, successor.ID).Scan(&personalCode, &personalEnds))
	s.Require().Equal(string(models.PlanCodePersonalPlus), personalCode)
	s.Require().True(personalEnds.Valid)
	s.Require().WithinDuration(endsAt, personalEnds.Time, time.Second)
}

// The previous owner leaves by default and keeps nothing the company gave them.
func (s *RepositorySuite) TestAcceptOwnershipRemovesThePreviousOwnerByDefault() {
	company, owner := s.createCompanyWithManager()
	successor := s.createUser(uuid.NewString() + "@example.com")

	created := s.offer(models.CompanyOwnershipTransfer{
		Scope:        models.CompanyOwnershipTransferScopeCompany,
		CompanyUUID:  uuid.NullUUID{UUID: company.ID, Valid: true},
		FromUserUUID: owner.ID,
		ToUserUUID:   successor.ID,
	})

	_, err := s.repository.AcceptOwnershipTransfer(s.ctx, created.ID, time.Now().UTC())
	s.Require().NoError(err)

	var status string
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT status FROM company_members WHERE company_uuid=$1 AND user_uuid=$2`, company.ID, owner.ID).Scan(&status))
	s.Require().Equal("left", status)
}

// ...and stays as an ordinary member when they asked to.
func (s *RepositorySuite) TestAcceptOwnershipKeepsThePreviousOwnerWhenAsked() {
	company, owner := s.createCompanyWithManager()
	successor := s.createUser(uuid.NewString() + "@example.com")

	created := s.offer(models.CompanyOwnershipTransfer{
		Scope:            models.CompanyOwnershipTransferScopeCompany,
		CompanyUUID:      uuid.NullUUID{UUID: company.ID, Valid: true},
		StayCompanyUUIDs: []uuid.UUID{company.ID},
		FromUserUUID:     owner.ID,
		ToUserUUID:       successor.ID,
	})
	s.Require().Equal([]uuid.UUID{company.ID}, created.StayCompanyUUIDs)

	_, err := s.repository.AcceptOwnershipTransfer(s.ctx, created.ID, time.Now().UTC())
	s.Require().NoError(err)

	var role, status string
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT role, status FROM company_members WHERE company_uuid=$1 AND user_uuid=$2`, company.ID, owner.ID).Scan(&role, &status))
	s.Require().Equal("employee", role)
	s.Require().Equal("active", status)
}

// An "all" offer covers every company the owner has when it is answered.
func (s *RepositorySuite) TestAcceptOwnershipMovesEveryCompanyOfTheOwner() {
	first, owner := s.createCompanyWithManager()
	second := s.createCompanyFor(owner)
	successor := s.createUser(uuid.NewString() + "@example.com")

	created := s.offer(models.CompanyOwnershipTransfer{
		Scope:        models.CompanyOwnershipTransferScopeAll,
		FromUserUUID: owner.ID,
		ToUserUUID:   successor.ID,
	})

	accepted, err := s.repository.AcceptOwnershipTransfer(s.ctx, created.ID, time.Now().UTC())
	s.Require().NoError(err)
	s.Require().ElementsMatch([]uuid.UUID{first.ID, second.ID}, accepted.CompanyUUIDs)

	var owned int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM companies WHERE manager_user_uuid=$1`, successor.ID).Scan(&owned))
	s.Require().Equal(2, owned)
}

// Somebody who already runs a company cannot take another one: one person holds
// one business plan, and two of them have no meaning the product could give.
func (s *RepositorySuite) TestAcceptOwnershipRefusesABusyRecipient() {
	company, owner := s.createCompanyWithManager()
	_, successorOwner := s.createCompanyWithManager()

	created := s.offer(models.CompanyOwnershipTransfer{
		Scope:        models.CompanyOwnershipTransferScopeCompany,
		CompanyUUID:  uuid.NullUUID{UUID: company.ID, Valid: true},
		FromUserUUID: owner.ID,
		ToUserUUID:   successorOwner.ID,
	})

	_, err := s.repository.AcceptOwnershipTransfer(s.ctx, created.ID, time.Now().UTC())
	s.Require().ErrorIs(err, models.ErrOwnershipRecipientBusy)
}

// Two overlapping offers would race for the same plan, so only one may wait.
func (s *RepositorySuite) TestOnlyOnePendingOfferPerOwner() {
	company, owner := s.createCompanyWithManager()
	second := s.createCompanyFor(owner)
	first := s.createUser(uuid.NewString() + "@example.com")
	other := s.createUser(uuid.NewString() + "@example.com")

	s.offer(models.CompanyOwnershipTransfer{
		Scope:        models.CompanyOwnershipTransferScopeCompany,
		CompanyUUID:  uuid.NullUUID{UUID: company.ID, Valid: true},
		FromUserUUID: owner.ID,
		ToUserUUID:   first.ID,
	})

	now := time.Now().UTC()
	_, err := s.repository.CreateOwnershipTransfer(s.ctx, models.CompanyOwnershipTransfer{
		ID:           uuid.New(),
		Scope:        models.CompanyOwnershipTransferScopeCompany,
		CompanyUUID:  uuid.NullUUID{UUID: second.ID, Valid: true},
		FromUserUUID: owner.ID,
		ToUserUUID:   other.ID,
		CreatedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
	})
	s.Require().ErrorIs(err, models.ErrCompanyOwnershipTransferPending)
}
