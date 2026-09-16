//go:build integration

package billing

import (
	"sync"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) TestListPlansIncludesDefaultPlans() {
	plans, err := s.repository.ListPlans(s.ctx)

	s.Require().NoError(err)
	s.Require().Len(plans, 6)
	s.Require().Equal(models.PlanCodePersonalStart, plans[0].Code)
	s.Require().Equal(180, plans[0].MonthlyMinutesLimit)
	s.Require().Equal(int64(250_000), plans[0].MonthlyCreditAllowance)
	s.Require().Equal(int64(0), plans[0].MonthlyPriceMinor)
	s.Require().Equal("RUB", plans[0].Currency)
	s.Require().Equal(3, plans[0].MarketingHoursHint)
	s.Require().Equal(2, plans[0].ActiveInstructionLimit)
	s.Require().Equal(models.PlanCodeBusinessPro, plans[5].Code)
	s.Require().NotNil(plans[5].CompanyLimit)
	s.Require().Equal(3, *plans[5].CompanyLimit)
	s.Require().NotNil(plans[5].InstructionsPerDepartmentLimit)
	s.Require().Equal(10, *plans[5].InstructionsPerDepartmentLimit)
	s.Require().True(plans[5].APIAccessEnabled)
	s.Require().True(plans[5].WebhooksEnabled)
	s.Require().Nil(plans[5].MembersPerCompanyLimit)
	s.Require().Equal(int64(9_990_000), plans[5].MonthlyPriceMinor)
	s.Require().Equal(int64(90_000_000), plans[5].MonthlyCreditAllowance)
}

func (s *RepositorySuite) TestEnsureCurrentCreditUsageIsSafeUnderConcurrentDashboardReads() {
	userID := s.createUser("credit-allowance-concurrent@example.com")
	subscription, err := s.repository.GetActivePersonalSubscription(s.ctx, userID)
	s.Require().NoError(err)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	start := make(chan struct{})
	errorsFound := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, callErr := s.repository.EnsureCurrentCreditUsage(s.ctx, subscription, now)
			errorsFound <- callErr
		}()
	}
	close(start)
	wg.Wait()
	close(errorsFound)
	for callErr := range errorsFound {
		s.Require().NoError(callErr)
	}
	var epochs, grants int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM allowance_epochs WHERE subscription_uuid=$1`, subscription.ID).Scan(&epochs))
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM credit_grants WHERE allowance_epoch_uuid IN (SELECT allowance_epoch_uuid FROM allowance_epochs WHERE subscription_uuid=$1)`, subscription.ID).Scan(&grants))
	s.Require().Equal(1, epochs)
	s.Require().Equal(1, grants)
}

// The plan belongs to the owner and covers the companies they own.
func (s *RepositorySuite) TestOwnerBusinessSubscriptionCoversTheirCompany() {
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

	covering, err := s.repository.GetActiveBusinessSubscription(s.ctx, companyID)
	s.Require().NoError(err)
	s.Require().Equal(created.ID, covering.ID)

	// A frozen company is not covered even while the owner keeps paying.
	_, err = s.db.ExecContext(s.ctx, `UPDATE companies SET lifecycle_state='frozen', frozen_at=now() WHERE company_uuid=$1`, companyID)
	s.Require().NoError(err)
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

	// One owner has one business plan: granting it again for another company
	// upgrades the same subscription instead of creating a second one.
	s.Require().Equal(start.ID, pro.ID)

	best, err := s.repository.GetBestActiveBusinessSubscriptionForManager(s.ctx, managerID)
	s.Require().NoError(err)
	s.Require().Equal(pro.ID, best.ID)
	s.Require().Equal(models.PlanCodeBusinessPro, best.Plan.Code)

	// Both companies are covered by that one plan.
	for _, companyID := range []uuid.UUID{firstCompanyID, secondCompanyID} {
		covering, coverErr := s.repository.GetActiveBusinessSubscription(s.ctx, companyID)
		s.Require().NoError(coverErr)
		s.Require().Equal(best.ID, covering.ID)
	}
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
	s.Require().True(created.UserUUID.Valid)
	s.Require().Equal(ownerID, created.UserUUID.UUID)

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
	subscription, err := s.repository.GetActivePersonalSubscription(s.ctx, userID)
	s.Require().NoError(err)
	s.Require().Equal(models.PlanCodePersonalStart, subscription.Plan.Code)

	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	_, err = s.repository.AddUsageMinutes(s.ctx, subscription.ID, now, 3)
	s.Require().NoError(err)
	_, err = s.repository.AddUsageMinutes(s.ctx, subscription.ID, now, 4)
	s.Require().NoError(err)

	usedMinutes, err := s.repository.CountUsedMinutes(s.ctx, subscription.ID, now)
	s.Require().NoError(err)
	s.Require().Equal(7, usedMinutes)
}

func (s *RepositorySuite) TestEnsureCurrentCreditUsageCreatesBalancedMonthlyAllowanceOnce() {
	userID := s.createUser("credit-allowance@example.com")
	subscription, err := s.repository.GetActivePersonalSubscription(s.ctx, userID)
	s.Require().NoError(err)
	s.Require().Equal(int64(250_000), subscription.Plan.MonthlyCreditAllowance)

	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	first, err := s.repository.EnsureCurrentCreditUsage(s.ctx, subscription, now)
	s.Require().NoError(err)
	s.Require().Equal(subscription.Plan.MonthlyCreditAllowance, first.AllowanceCredits)
	s.Require().Equal(subscription.Plan.MonthlyCreditAllowance, first.AllowanceRemaining)
	s.Require().Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), first.ResetsAt)

	second, err := s.repository.EnsureCurrentCreditUsage(s.ctx, subscription, now.Add(time.Hour))
	s.Require().NoError(err)
	s.Require().Equal(first, second)

	var epochs, grants, transactions int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM allowance_epochs WHERE subscription_uuid=$1`, subscription.ID).Scan(&epochs))
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM credit_grants WHERE source_reference LIKE 'allowance:%'`).Scan(&grants))
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM credit_ledger_transactions WHERE transaction_type='allowance_opened'`).Scan(&transactions))
	s.Require().Equal(1, epochs)
	s.Require().Equal(1, grants)
	s.Require().Equal(1, transactions)

	var postingSum int64
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT COALESCE(sum(amount_credits),0) FROM credit_ledger_postings`).Scan(&postingSum))
	s.Require().Zero(postingSum)
}

func (s *RepositorySuite) TestReserveAndSettleCreditsNeverExceedsMaximumCharge() {
	userID := s.createUser("credit-settlement@example.com")
	subscription, err := s.repository.GetActivePersonalSubscription(s.ctx, userID)
	s.Require().NoError(err)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	operationID := uuid.New()
	reserved, err := s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{
		OperationUUID: operationID, OperationType: "analysis", Environment: "production",
		Provider: "openrouter", Model: "openai/gpt-5-mini", IdempotencyKey: "analysis:test:1",
		MaximumCharge: 1_000,
	}, now)
	s.Require().NoError(err)
	s.Require().Equal(int64(1_000), reserved.ReservedCredits)

	duplicate, err := s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{
		OperationUUID: operationID, OperationType: "analysis", Environment: "production",
		Provider: "openrouter", Model: "openai/gpt-5-mini", IdempotencyKey: "analysis:test:1",
		MaximumCharge: 1_000,
	}, now)
	s.Require().NoError(err)
	s.Require().Equal(reserved, duplicate)

	settled, err := s.repository.SettleCredits(s.ctx, models.SettleCreditsInput{
		OperationUUID: operationID, ActualChargeCredits: 1_250,
		ProviderCostNanoUSD: 3_500_000, ProviderUsageJSON: []byte(`{"total_tokens":100}`),
	}, now.Add(time.Second))
	s.Require().NoError(err)
	s.Require().Equal(int64(1_000), settled.SettledCredits)
	s.Require().Equal(int64(250), settled.InternalPricingLossCredits)
	s.Require().Zero(settled.ReservedCredits)

	usage, err := s.repository.EnsureCurrentCreditUsage(s.ctx, subscription, now.Add(time.Minute))
	s.Require().NoError(err)
	s.Require().Equal(subscription.Plan.MonthlyCreditAllowance-1_000, usage.AllowanceRemaining)

	var ledgerSum, reservedBalance int64
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT COALESCE(sum(amount_credits),0) FROM credit_ledger_postings`).Scan(&ledgerSum))
	s.Require().Zero(ledgerSum)
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `
		SELECT COALESCE(sum(p.amount_credits),0)
		FROM credit_ledger_postings p JOIN credit_ledger_accounts a USING(credit_ledger_account_uuid)
		WHERE a.operation_uuid=$1 AND a.account_type='customer_reserved'
	`, operationID).Scan(&reservedBalance))
	s.Require().Zero(reservedBalance)
}

func (s *RepositorySuite) TestReserveCreditsRejectsPartialFunding() {
	userID := s.createUser("credit-insufficient@example.com")
	subscription, err := s.repository.GetActivePersonalSubscription(s.ctx, userID)
	s.Require().NoError(err)
	_, err = s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{
		OperationUUID: uuid.New(), OperationType: "analysis", Environment: "production",
		Provider: "openrouter", Model: "openai/gpt-5-mini", IdempotencyKey: "analysis:too-expensive",
		MaximumCharge: subscription.Plan.MonthlyCreditAllowance + 1,
	}, time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC))
	s.Require().ErrorIs(err, models.ErrInsufficientCredits)

	var operations int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM usage_operations`).Scan(&operations))
	s.Require().Zero(operations)
}

func (s *RepositorySuite) TestReserveCreditsEnforcesApplicationBudgetsAtomically() {
	userID := s.createUser("application-budget@example.com")
	subscription, err := s.repository.GetActivePersonalSubscription(s.ctx, userID)
	s.Require().NoError(err)
	daily, monthly, perOperation := int64(1_500), int64(2_000), int64(1_000)
	app, err := s.repository.CreateDeveloperApplication(s.ctx, models.CreateDeveloperApplicationInput{
		OwnerType: "user", OwnerUUID: userID, CreatedByUserUUID: userID,
		Name: "Production API", Environment: "production", Capabilities: []string{"calls:write"},
		DailyCreditLimit: &daily, MonthlyCreditLimit: &monthly, MaxCreditsPerOperation: &perOperation,
	})
	s.Require().NoError(err)
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)

	_, err = s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{
		OperationUUID: uuid.New(), ApplicationUUID: uuid.NullUUID{UUID: app.ID, Valid: true}, OperationType: "analysis", Environment: "production",
		Provider: "openrouter", Model: "openai/gpt-5-mini", IdempotencyKey: "budget:first", MaximumCharge: 900,
	}, now)
	s.Require().NoError(err)

	_, err = s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{
		OperationUUID: uuid.New(), ApplicationUUID: uuid.NullUUID{UUID: app.ID, Valid: true}, OperationType: "analysis", Environment: "production",
		Provider: "openrouter", Model: "openai/gpt-5-mini", IdempotencyKey: "budget:per-operation", MaximumCharge: 1_001,
	}, now)
	s.Require().ErrorIs(err, models.ErrApplicationBudgetExceeded)

	_, err = s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{
		OperationUUID: uuid.New(), ApplicationUUID: uuid.NullUUID{UUID: app.ID, Valid: true}, OperationType: "analysis", Environment: "production",
		Provider: "openrouter", Model: "openai/gpt-5-mini", IdempotencyKey: "budget:daily", MaximumCharge: 601,
	}, now)
	s.Require().ErrorIs(err, models.ErrApplicationBudgetExceeded)
}

func (s *RepositorySuite) TestDeveloperApplicationSeparatesSandboxAndRevealsKeyOnce() {
	managerID := s.createUser("developer-platform@example.com")
	companyID := s.createCompany(managerID)
	app, err := s.repository.CreateDeveloperApplication(s.ctx, models.CreateDeveloperApplicationInput{
		OwnerType: "company", OwnerUUID: companyID, CreatedByUserUUID: managerID,
		Name: "CRM sandbox", Environment: "sandbox", Capabilities: []string{"calls:write", "usage:read"},
	})
	s.Require().NoError(err)
	s.Require().Equal("sandbox", app.Environment)

	apps, err := s.repository.ListDeveloperApplications(s.ctx, "company", companyID)
	s.Require().NoError(err)
	s.Require().Len(apps, 1)
	s.Require().ElementsMatch([]string{"calls:write", "usage:read"}, apps[0].Capabilities)
	s.Require().Equal(int64(1), apps[0].LockVersion)
	updated, err := s.repository.UpdateDeveloperApplication(s.ctx, models.UpdateDeveloperApplicationInput{ApplicationUUID: app.ID, ActorUUID: managerID, Name: "CRM sandbox updated", Capabilities: []string{"calls:write", "usage:read"}, ExpectedLockVersion: apps[0].LockVersion})
	s.Require().NoError(err)
	s.Require().Equal("CRM sandbox updated", updated.Name)
	s.Require().Equal(int64(2), updated.LockVersion)
	_, err = s.repository.UpdateDeveloperApplication(s.ctx, models.UpdateDeveloperApplicationInput{ApplicationUUID: app.ID, ActorUUID: managerID, Name: "stale", Capabilities: []string{"calls:write", "usage:read"}, ExpectedLockVersion: 1})
	s.Require().ErrorIs(err, models.ErrIntegrationConflict)

	key, plaintext, err := s.repository.CreateIntegrationAPIKey(s.ctx, app.ID, managerID, models.CreateIntegrationAPIKeyInput{Name: "CI key", Scopes: []string{"calls:write"}})
	s.Require().NoError(err)
	s.Require().NotEmpty(plaintext)
	s.Require().Contains(plaintext, "vt_test_")
	s.Require().NotContains(key.Prefix, ".")
	var storedSecret string
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT encode(secret_hash,'hex') FROM integration_api_keys WHERE key_uuid=$1`, key.ID).Scan(&storedSecret))
	s.Require().NotEqual(plaintext, storedSecret)
	principal, err := s.repository.AuthenticateIntegrationKey(s.ctx, plaintext, "sandbox", "calls:write")
	s.Require().NoError(err)
	s.Require().Equal(app.ID, principal.ApplicationUUID)
	_, err = s.repository.AuthenticateIntegrationKey(s.ctx, plaintext, "production", "calls:write")
	s.Require().ErrorIs(err, models.ErrAPIKeyEnvironmentMismatch)
	accounts, err := s.repository.ListIntegrationServiceAccounts(s.ctx, principal.ConnectionUUID, managerID)
	s.Require().NoError(err)
	s.Require().Len(accounts, 1)
	serviceAccount, err := s.repository.CreateIntegrationServiceAccount(s.ctx, principal.ConnectionUUID, managerID, "Partner worker", []string{"calls:write", "usage:read"})
	s.Require().NoError(err)
	permanentLimit := int64(50_000)
	partnerKey, partnerPlaintext, err := s.repository.CreateIntegrationAPIKeyForServiceAccount(s.ctx, serviceAccount.ID, managerID, models.CreateIntegrationAPIKeyInput{Name: "Partner key limited", Scopes: []string{"calls:write"}, PermanentCreditLimit: &permanentLimit})
	s.Require().NoError(err)
	s.Require().Equal(serviceAccount.ID, partnerKey.ServiceAccountID)
	s.Require().Equal(&permanentLimit, partnerKey.PermanentCreditLimit)
	_, err = s.repository.AuthenticateIntegrationKey(s.ctx, partnerPlaintext, "sandbox", "calls:write")
	s.Require().NoError(err)
	rotated, rotatedPlaintext, err := s.repository.RotateIntegrationAPIKey(s.ctx, partnerKey.ID, managerID, 0)
	s.Require().NoError(err)
	s.Require().Equal(serviceAccount.ID, rotated.ServiceAccountID)
	s.Require().Equal(&permanentLimit, rotated.PermanentCreditLimit)
	_, err = s.repository.AuthenticateIntegrationKey(s.ctx, partnerPlaintext, "sandbox", "calls:write")
	s.Require().ErrorIs(err, models.ErrInvalidAPIKey)
	_, err = s.repository.AuthenticateIntegrationKey(s.ctx, rotatedPlaintext, "sandbox", "calls:write")
	s.Require().NoError(err)
	s.Require().NoError(s.repository.RevokeIntegrationAPIKey(s.ctx, rotated.ID, managerID))
	_, err = s.repository.AuthenticateIntegrationKey(s.ctx, rotatedPlaintext, "sandbox", "calls:write")
	s.Require().ErrorIs(err, models.ErrInvalidAPIKey)
	s.Require().NoError(s.repository.RevokeIntegrationServiceAccount(s.ctx, serviceAccount.ID, managerID))
	_, _, err = s.repository.CreateIntegrationAPIKeyForServiceAccount(s.ctx, serviceAccount.ID, managerID, models.CreateIntegrationAPIKeyInput{Name: "must fail", Scopes: []string{"calls:write"}})
	s.Require().ErrorIs(err, models.ErrForbidden)

	var sandboxBalance int64
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT COALESCE(sum(p.amount_credits),0) FROM credit_ledger_postings p JOIN credit_ledger_accounts a USING(credit_ledger_account_uuid) WHERE a.billing_account_uuid=$1 AND a.environment='sandbox' AND a.account_type='customer_available'`, app.BillingAccountUUID).Scan(&sandboxBalance))
	s.Require().Equal(int64(100_000), sandboxBalance)
}

func (s *RepositorySuite) TestSandboxWalletIsApplicationScopedAndAdjustmentsAreIdempotent() {
	userID := s.createUser("sandbox-wallet@example.com")
	first, err := s.repository.CreateDeveloperApplication(s.ctx, models.CreateDeveloperApplicationInput{OwnerType: "user", OwnerUUID: userID, CreatedByUserUUID: userID, Name: "First sandbox", Environment: "sandbox", Capabilities: []string{"calls:write"}})
	s.Require().NoError(err)
	second, err := s.repository.CreateDeveloperApplication(s.ctx, models.CreateDeveloperApplicationInput{OwnerType: "user", OwnerUUID: userID, CreatedByUserUUID: userID, Name: "Second sandbox", Environment: "sandbox", Capabilities: []string{"calls:write"}})
	s.Require().NoError(err)
	balance, err := s.repository.AdjustSandboxWallet(s.ctx, first.ID, userID, "add", 25_000, "add-1")
	s.Require().NoError(err)
	s.Require().Equal(int64(125_000), balance)
	balance, err = s.repository.AdjustSandboxWallet(s.ctx, first.ID, userID, "add", 25_000, "add-1")
	s.Require().NoError(err)
	s.Require().Equal(int64(125_000), balance)
	balance, err = s.repository.AdjustSandboxWallet(s.ctx, first.ID, userID, "set", 10_000, "set-1")
	s.Require().NoError(err)
	s.Require().Equal(int64(10_000), balance)
	balance, err = s.repository.AdjustSandboxWallet(s.ctx, first.ID, userID, "reset", 0, "reset-1")
	s.Require().NoError(err)
	s.Require().Equal(int64(100_000), balance)
	wallet, err := s.repository.GetSandboxWallet(s.ctx, first.ID, userID)
	s.Require().NoError(err)
	s.Require().Equal("First sandbox", wallet.ApplicationName)
	s.Require().Equal(int64(100_000), wallet.BalanceCredits)
	s.Require().Len(wallet.Entries, 4)
	outsider := s.createUser("sandbox-wallet-outsider@example.com")
	_, err = s.repository.GetSandboxWallet(s.ctx, first.ID, outsider)
	s.Require().ErrorIs(err, models.ErrForbidden)
	var secondBalance int64
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT COALESCE(sum(p.amount_credits),0) FROM credit_grants g JOIN credit_ledger_accounts a USING(credit_grant_uuid) LEFT JOIN credit_ledger_postings p USING(credit_ledger_account_uuid) WHERE g.application_uuid=$1 AND a.account_type='customer_available'`, second.ID).Scan(&secondBalance))
	s.Require().Equal(int64(100_000), secondBalance)
}

func (s *RepositorySuite) TestReconciliationReleasesReservationThatNeverReachedProvider() {
	userID := s.createUser("reconcile-reserve@example.com")
	subscription, err := s.repository.GetActivePersonalSubscription(s.ctx, userID)
	s.Require().NoError(err)
	now := time.Now().UTC()
	_, err = s.repository.EnsureCurrentCreditUsage(s.ctx, subscription, now)
	s.Require().NoError(err)
	operationID := uuid.New()
	_, err = s.repository.ReserveCredits(s.ctx, subscription, models.ReserveCreditsInput{OperationUUID: operationID, OperationType: "analysis", Environment: "production", Provider: "openrouter", Model: "openai/gpt-5-mini", IdempotencyKey: "reconcile-before-provider", MaximumCharge: 1000}, now)
	s.Require().NoError(err)
	summary, err := s.repository.ReconcileCreditOperations(s.ctx, now.Add(time.Minute), 100)
	s.Require().NoError(err)
	s.Require().GreaterOrEqual(summary.Checked, int64(1))
	var status string
	var reserved, settled int64
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT status,reserved_credits,settled_credits FROM usage_operations WHERE usage_operation_uuid=$1`, operationID).Scan(&status, &reserved, &settled))
	s.Require().Equal("settled", status)
	s.Require().Zero(reserved)
	s.Require().Zero(settled)
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
		SELECT user_uuid, 'Dmitry', 'Mukhachev', $4 FROM account`,
		id,
		email,
		time.Now().UTC(),
		"muxa-"+id.String()[:8],
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
