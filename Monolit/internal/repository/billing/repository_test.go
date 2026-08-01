//go:build integration

package billing

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) TestListPlansIncludesDefaultPlans() {
	plans, err := s.repository.ListPlans(s.ctx)

	s.Require().NoError(err)
	s.Require().Len(plans, 6)
	s.Require().Equal(models.PlanCodePersonalStart, plans[0].Code)
	s.Require().Equal(120, plans[0].MonthlyMinutesLimit)
	s.Require().Equal(2, plans[0].ActiveInstructionLimit)
	s.Require().Equal(models.PlanCodeBusinessPro, plans[5].Code)
	s.Require().NotNil(plans[5].CompanyLimit)
	s.Require().Equal(3, *plans[5].CompanyLimit)
	s.Require().NotNil(plans[5].InstructionsPerDepartmentLimit)
	s.Require().Equal(10, *plans[5].InstructionsPerDepartmentLimit)
	s.Require().True(plans[5].APIAccessEnabled)
}

func (s *RepositorySuite) TestUserBusinessSubscriptionDoesNotCoverCompany() {
	managerID := s.createUser("manager-business-user@example.com")
	companyID := s.createCompany(managerID)

	created, err := s.repository.UpsertSubscription(s.ctx, models.UpsertSubscriptionInput{
		PlanCode: models.PlanCodeBusinessPro,
		UserUUID: uuid.NullUUID{UUID: managerID, Valid: true},
		Status:   models.SubscriptionStatusActive,
		StartsAt: time.Now().UTC().Add(-time.Hour),
	})
	s.Require().NoError(err)
	s.Require().Equal(models.PlanCodeBusinessPro, created.Plan.Code)

	_, err = s.repository.GetActiveBusinessSubscription(s.ctx, companyID)
	s.Require().ErrorIs(err, models.ErrSubscriptionNotFound)
}

func (s *RepositorySuite) TestGetBestActiveBusinessSubscriptionForManager() {
	managerID := s.createUser("best-business-manager@example.com")
	firstCompanyID := s.createCompany(managerID)
	secondCompanyID := s.createCompany(managerID)

	start, err := s.repository.ActivateCompanySubscription(s.ctx, models.ActivateCompanySubscriptionInput{
		CompanyUUID: firstCompanyID,
		PlanCode:    models.PlanCodeBusinessStart,
	}, time.Now().UTC().Add(-time.Hour))
	s.Require().NoError(err)

	pro, err := s.repository.ActivateCompanySubscription(s.ctx, models.ActivateCompanySubscriptionInput{
		CompanyUUID: secondCompanyID,
		PlanCode:    models.PlanCodeBusinessPro,
	}, time.Now().UTC().Add(-time.Hour))
	s.Require().NoError(err)

	best, err := s.repository.GetBestActiveBusinessSubscriptionForManager(s.ctx, managerID)
	s.Require().NoError(err)
	s.Require().Equal(pro.ID, best.ID)
	s.Require().NotEqual(start.ID, best.ID)
}

func (s *RepositorySuite) TestActivateAndCancelCompanySubscription() {
	ownerID := s.createUser("company-subscription-owner@example.com")
	companyID := s.createCompany(ownerID)

	startsAt := time.Now().UTC().Add(-time.Hour)
	created, err := s.repository.ActivateCompanySubscription(s.ctx, models.ActivateCompanySubscriptionInput{
		CompanyUUID: companyID,
		PlanCode:    models.PlanCodeBusinessPlus,
	}, startsAt)
	s.Require().NoError(err)
	s.Require().Equal(models.PlanCodeBusinessPlus, created.Plan.Code)
	s.Require().Equal(models.SubscriptionStatusActive, created.Status)
	s.Require().True(created.CompanyUUID.Valid)
	s.Require().Equal(companyID, created.CompanyUUID.UUID)

	updated, err := s.repository.ActivateCompanySubscription(s.ctx, models.ActivateCompanySubscriptionInput{
		CompanyUUID: companyID,
		PlanCode:    models.PlanCodeBusinessPro,
	}, startsAt.Add(time.Minute))
	s.Require().NoError(err)
	s.Require().Equal(created.ID, updated.ID)
	s.Require().Equal(models.PlanCodeBusinessPro, updated.Plan.Code)

	active, err := s.repository.GetActiveBusinessSubscription(s.ctx, companyID)
	s.Require().NoError(err)
	s.Require().Equal(updated.ID, active.ID)

	canceled, err := s.repository.CancelCompanySubscription(s.ctx, companyID, time.Now().UTC())
	s.Require().NoError(err)
	s.Require().Equal(updated.ID, canceled.ID)
	s.Require().Equal(models.SubscriptionStatusCanceled, canceled.Status)
	s.Require().NotNil(canceled.EndsAt)

	_, err = s.repository.GetActiveBusinessSubscription(s.ctx, companyID)
	s.Require().ErrorIs(err, models.ErrSubscriptionNotFound)
}

func (s *RepositorySuite) TestAddUsageMinutesAccumulatesCurrentPeriod() {
	userID := s.createUser("usage@example.com")
	subscription, err := s.repository.UpsertSubscription(s.ctx, models.UpsertSubscriptionInput{
		PlanCode: models.PlanCodePersonalStart,
		UserUUID: uuid.NullUUID{UUID: userID, Valid: true},
		Status:   models.SubscriptionStatusActive,
		StartsAt: time.Now().UTC().Add(-time.Hour),
	})
	s.Require().NoError(err)

	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	_, err = s.repository.AddUsageMinutes(s.ctx, subscription.ID, now, 3)
	s.Require().NoError(err)
	_, err = s.repository.AddUsageMinutes(s.ctx, subscription.ID, now, 4)
	s.Require().NoError(err)

	usedMinutes, err := s.repository.CountUsedMinutes(s.ctx, subscription.ID, now)
	s.Require().NoError(err)
	s.Require().Equal(7, usedMinutes)
}

func (s *RepositorySuite) TestPersonalSubscriptionPlanAndUsageLifecycle() {
	userID := s.createUser("personal-subscription@example.com")
	plan, err := s.repository.GetPlanByCode(s.ctx, models.PlanCodePersonalPlus)
	s.Require().NoError(err)
	s.Require().Equal(models.PlanTypePersonal, plan.Type)

	created, err := s.repository.ActivatePersonalSubscription(s.ctx, models.ActivatePersonalSubscriptionInput{
		UserUUID: userID,
		PlanCode: models.PlanCodePersonalStart,
	}, time.Time{})
	s.Require().NoError(err)
	s.Require().Equal(models.PlanCodePersonalStart, created.Plan.Code)

	updated, err := s.repository.ActivatePersonalSubscription(s.ctx, models.ActivatePersonalSubscriptionInput{
		UserUUID: userID,
		PlanCode: models.PlanCodePersonalPro,
	}, time.Now().UTC().Add(-time.Hour))
	s.Require().NoError(err)
	s.Require().Equal(created.ID, updated.ID)
	s.Require().Equal(models.PlanCodePersonalPro, updated.Plan.Code)

	active, err := s.repository.GetActivePersonalSubscription(s.ctx, userID)
	s.Require().NoError(err)
	s.Require().Equal(updated.ID, active.ID)

	period := time.Date(2026, 6, 22, 12, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	counter, err := s.repository.AddUsageMinutes(s.ctx, active.ID, period, 0)
	s.Require().NoError(err)
	s.Require().Zero(counter.UsedMinutes)
	counter, err = s.repository.GetUsageCounter(s.ctx, active.ID, period)
	s.Require().NoError(err)
	s.Require().Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), counter.PeriodStart)

	used, err := s.repository.CountUsedMinutes(s.ctx, active.ID, period.AddDate(0, 1, 0))
	s.Require().NoError(err)
	s.Require().Zero(used)

	_, err = s.repository.GetPlanByCode(s.ctx, "missing")
	s.Require().ErrorIs(err, models.ErrPlanNotFound)
	_, err = s.repository.ActivatePersonalSubscription(s.ctx, models.ActivatePersonalSubscriptionInput{
		UserUUID: userID, PlanCode: "missing",
	}, time.Now())
	s.Require().ErrorIs(err, models.ErrPlanNotFound)
	_, err = s.repository.GetUsageCounter(s.ctx, uuid.New(), period)
	s.Require().ErrorIs(err, models.ErrSubscriptionNotFound)
}

func (s *RepositorySuite) TestResourceCountsAndMissingCompanySubscription() {
	ownerID := s.createUser("counts-owner@example.com")
	companyID := s.createCompany(ownerID)
	memberID := uuid.New()
	_, err := s.db.ExecContext(s.ctx, `
		WITH account AS (
			INSERT INTO users (user_uuid, email, password_hash, role, created_at)
			VALUES ($1, $2, 'hash', 'user', now())
			RETURNING user_uuid
		)
		INSERT INTO user_profiles (user_uuid, full_name, full_surname, username)
		SELECT user_uuid, 'Member', 'User', $3 FROM account`,
		memberID, memberID.String()+"@example.com", "member_"+memberID.String()[:8])
	s.Require().NoError(err)
	departmentID := uuid.New()
	_, err = s.db.ExecContext(s.ctx,
		`INSERT INTO departments (department_uuid, company_uuid, name, created_at) VALUES ($1, $2, 'Sales', now())`,
		departmentID, companyID)
	s.Require().NoError(err)
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO company_members (company_uuid, user_uuid, role, status, created_at)
		VALUES ($1, $2, 'employee', 'active', now())`, companyID, memberID)
	s.Require().NoError(err)
	instructionID := uuid.New()
	_, err = s.db.ExecContext(s.ctx, `
		INSERT INTO analysis_instructions (
			instruction_uuid, scope, user_uuid, title, original_filename, file_path,
			mime_type, size_bytes, content_sha256, sort_order, is_active,
			created_by_user_uuid, created_at, updated_at
		) VALUES ($1, 'personal', $2, 'Rubric', 'rubric.txt', 'instructions/rubric.txt',
			'text/plain', 10, 'hash', 0, true, $2, now(), now())`,
		instructionID, ownerID)
	s.Require().NoError(err)

	count, err := s.repository.CountOwnerCompanies(s.ctx, ownerID)
	s.Require().NoError(err)
	s.Require().Equal(1, count)
	count, err = s.repository.CountCompanyDepartments(s.ctx, companyID)
	s.Require().NoError(err)
	s.Require().Equal(1, count)
	count, err = s.repository.CountCompanyMembers(s.ctx, companyID)
	s.Require().NoError(err)
	s.Require().Equal(1, count)
	count, err = s.repository.CountActiveInstructions(s.ctx, models.ListAnalysisInstructionsInput{
		Scope: models.AnalysisInstructionScopePersonal, UserUUID: ownerID,
	})
	s.Require().NoError(err)
	s.Require().Equal(1, count)

	_, err = s.repository.CancelCompanySubscription(s.ctx, companyID, time.Time{})
	s.Require().ErrorIs(err, models.ErrSubscriptionNotFound)
	_, err = s.repository.ActivateCompanySubscription(s.ctx, models.ActivateCompanySubscriptionInput{
		CompanyUUID: companyID, PlanCode: models.PlanCodePersonalStart,
	}, time.Time{})
	s.Require().ErrorIs(err, models.ErrPlanNotFound)
}

func (s *RepositorySuite) createUser(email string) uuid.UUID {
	id := uuid.New()
	_, err := s.db.ExecContext(
		s.ctx,
		`WITH account AS (
			INSERT INTO users (user_uuid, email, password_hash, role, created_at)
			VALUES ($1, $2, 'hash', 'user', $3)
			RETURNING user_uuid
		)
		INSERT INTO user_profiles (user_uuid, full_name, full_surname, username)
		SELECT user_uuid, 'Dmitry', 'Mukhachev', 'muxa' FROM account`,
		id,
		email,
		time.Now().UTC(),
	)
	s.Require().NoError(err)

	return id
}

func (s *RepositorySuite) createCompany(ownerID uuid.UUID) uuid.UUID {
	companyID := uuid.New()
	_, err := s.db.ExecContext(
		s.ctx,
		`INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, member_limit, created_at)
		 VALUES ($1, 'VerbaTrace', $2, $3, 25, $4)`,
		companyID,
		"@"+companyID.String(),
		ownerID,
		time.Now().UTC(),
	)
	s.Require().NoError(err)

	return companyID
}
