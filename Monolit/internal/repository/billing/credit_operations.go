package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type grantBalance struct {
	grantID   uuid.UUID
	accountID uuid.UUID
	available int64
}

func (r *Repository) IntegrationBillingContextForCall(ctx context.Context, callID uuid.UUID) (uuid.NullUUID, string, error) {
	var applicationID uuid.NullUUID
	var environment sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT i.application_uuid,i.billing_environment FROM calls c JOIN ingest_items i ON i.ingest_item_uuid=c.ingest_item_uuid WHERE c.call_uuid=$1`, callID).Scan(&applicationID, &environment)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.NullUUID{}, "production", nil
	}
	if err != nil {
		return uuid.NullUUID{}, "", err
	}
	return applicationID, environment.String, nil
}

func (r *Repository) IsSandboxMockCall(ctx context.Context, callID uuid.UUID) (bool, error) {
	var result bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM calls c JOIN ingest_items i ON i.ingest_item_uuid=c.ingest_item_uuid WHERE c.call_uuid=$1 AND i.ai_mode='mock')`, callID).Scan(&result)
	return result, err
}

// ReserveCredits reserves the full disclosed maximum before a provider call.
// It never performs a partial reserve.
func (r *Repository) ReserveCredits(ctx context.Context, subscription models.Subscription, input models.ReserveCreditsInput, now time.Time) (models.CreditOperation, error) {
	if input.OperationUUID == uuid.Nil || strings.TrimSpace(input.IdempotencyKey) == "" || input.MaximumCharge < 0 {
		return models.CreditOperation{}, models.ErrInvalidBillingInput
	}
	if input.Environment != "production" && input.Environment != "sandbox" {
		return models.CreditOperation{}, models.ErrInvalidBillingInput
	}
	if _, err := r.EnsureCurrentCreditUsage(ctx, subscription, now); err != nil {
		return models.CreditOperation{}, err
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.CreditOperation{}, fmt.Errorf("begin credit reserve: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	accountID, err := billingAccountForSubscription(ctx, tx, subscription)
	if err != nil {
		return models.CreditOperation{}, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM billing_accounts WHERE billing_account_uuid=$1 FOR UPDATE`, accountID); err != nil {
		return models.CreditOperation{}, fmt.Errorf("lock billing account: %w", err)
	}
	if existing, found, lookupErr := findCreditOperationByIdempotency(ctx, tx, accountID, input.Environment, input.IdempotencyKey); lookupErr != nil {
		return models.CreditOperation{}, lookupErr
	} else if found {
		if existing.ID != input.OperationUUID || existing.MaximumChargeCredits != input.MaximumCharge {
			return models.CreditOperation{}, models.ErrCreditOperationConflict
		}
		return existing, nil
	}
	if input.ApplicationUUID.Valid {
		if err = enforceApplicationBudget(ctx, tx, input.ApplicationUUID.UUID, accountID, input.Environment, input.MaximumCharge, now); err != nil {
			return models.CreditOperation{}, err
		}
	}
	keyID, err := integrationKeyForCall(ctx, tx, input.CallUUID)
	if err != nil {
		return models.CreditOperation{}, err
	}
	if keyID.Valid {
		if err = enforceIntegrationKeyBudget(ctx, tx, keyID.UUID, input.MaximumCharge, now); err != nil {
			return models.CreditOperation{}, err
		}
	}

	pricingID, err := activePricingCatalog(ctx, tx, now)
	if err != nil {
		return models.CreditOperation{}, err
	}
	grants, total, err := spendableGrants(ctx, tx, accountID, input.ApplicationUUID.UUID, input.Environment, now)
	if err != nil {
		return models.CreditOperation{}, err
	}
	if total < input.MaximumCharge {
		return models.CreditOperation{}, models.ErrInsufficientCredits
	}

	if input.Environment == "production" && input.CompanyUUID.Valid {
		period := models.CreditPeriodFor(subscription.StartsAt, now)
		if err = checkCreditLimits(ctx, tx, input, period.Start, period.End); err != nil {
			return models.CreditOperation{}, err
		}
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO usage_operations(
			usage_operation_uuid,billing_account_uuid,application_uuid,key_uuid,call_uuid,
			company_uuid,department_uuid,
			operation_type,environment,provider,model,mode,pricing_catalog_version_uuid,
			idempotency_key,status,maximum_charge_credits,reserved_credits,started_at
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'reserved',$15,$15,$16)
	`, input.OperationUUID, accountID, nullableUUID(input.ApplicationUUID), nullableUUID(keyID), nullableUUID(input.CallUUID),
		nullableUUID(input.CompanyUUID), nullableUUID(input.DepartmentUUID),
		input.OperationType, input.Environment, input.Provider, input.Model, input.Mode,
		pricingID, input.IdempotencyKey, input.MaximumCharge, now.UTC()); err != nil {
		return models.CreditOperation{}, fmt.Errorf("create usage operation: %w", err)
	}

	reservedID, err := ensureOperationAccount(ctx, tx, accountID, input.OperationUUID, input.Environment, "customer_reserved")
	if err != nil {
		return models.CreditOperation{}, err
	}
	if input.MaximumCharge > 0 {
		transactionID, idErr := uuid.NewV7()
		if idErr != nil {
			return models.CreditOperation{}, idErr
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO credit_ledger_transactions(
				credit_ledger_transaction_uuid,transaction_type,idempotency_key,actor_type,reason,source_reference
			) VALUES($1,'reserve',$2,'system','maximum charge reserved',$3)
		`, transactionID, "reserve:"+input.IdempotencyKey, input.OperationUUID.String()); err != nil {
			return models.CreditOperation{}, fmt.Errorf("create reserve transaction: %w", err)
		}
		remaining := input.MaximumCharge
		order := 0
		for _, grant := range grants {
			if remaining == 0 {
				break
			}
			amount := minInt64(remaining, grant.available)
			if amount == 0 {
				continue
			}
			order++
			if _, err = tx.ExecContext(ctx, `
				INSERT INTO credit_ledger_postings(
					credit_ledger_posting_uuid,credit_ledger_transaction_uuid,credit_ledger_account_uuid,amount_credits
				) VALUES(gen_random_uuid(),$1,$2,-($4::bigint)),(gen_random_uuid(),$1,$3,$4::bigint)
			`, transactionID, grant.accountID, reservedID, amount); err != nil {
				return models.CreditOperation{}, fmt.Errorf("post reserve transaction: %w", err)
			}
			if _, err = tx.ExecContext(ctx, `
				INSERT INTO usage_operation_reservations(
					usage_operation_reservation_uuid,usage_operation_uuid,credit_grant_uuid,reserved_credits,spend_order
				) VALUES(gen_random_uuid(),$1,$2,$3,$4)
			`, input.OperationUUID, grant.grantID, amount, order); err != nil {
				return models.CreditOperation{}, fmt.Errorf("record reserve allocation: %w", err)
			}
			remaining -= amount
		}
	}

	result := models.CreditOperation{
		ID: input.OperationUUID, BillingAccountUUID: accountID, Status: "reserved",
		MaximumChargeCredits: input.MaximumCharge, ReservedCredits: input.MaximumCharge,
	}
	if err = tx.Commit(); err != nil {
		return models.CreditOperation{}, fmt.Errorf("commit credit reserve: %w", err)
	}
	return result, nil
}

func integrationKeyForCall(ctx context.Context, tx *sql.Tx, callID uuid.NullUUID) (uuid.NullUUID, error) {
	if !callID.Valid {
		return uuid.NullUUID{}, nil
	}
	var key uuid.NullUUID
	err := tx.QueryRowContext(ctx, `SELECT i.key_uuid FROM ingest_items i WHERE i.call_uuid=$1`, callID.UUID).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.NullUUID{}, nil
	}
	return key, err
}

func enforceIntegrationKeyBudget(ctx context.Context, tx *sql.Tx, keyID uuid.UUID, maximumCharge int64, now time.Time) error {
	var permanent, temporary sql.NullInt64
	var starts, ends sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT permanent_credit_limit,temporary_credit_limit,temporary_limit_starts_at,temporary_limit_ends_at FROM integration_api_keys WHERE key_uuid=$1 AND revoked_at IS NULL FOR UPDATE`, keyID).Scan(&permanent, &temporary, &starts, &ends)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrInvalidAPIKey
	}
	if err != nil {
		return err
	}
	var lifetimeUsed int64
	if permanent.Valid {
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(CASE WHEN status='settled' THEN settled_credits ELSE reserved_credits END),0) FROM usage_operations WHERE key_uuid=$1 AND status IN ('reserved','provider_running','settled','reconciling')`, keyID).Scan(&lifetimeUsed)
		if err != nil {
			return err
		}
		if maximumCharge > permanent.Int64-lifetimeUsed {
			return models.ErrApplicationBudgetExceeded
		}
	}
	if temporary.Valid && starts.Valid && ends.Valid && !now.Before(starts.Time) && now.Before(ends.Time) {
		var windowUsed int64
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(CASE WHEN status='settled' THEN settled_credits ELSE reserved_credits END),0) FROM usage_operations WHERE key_uuid=$1 AND started_at >= $2 AND started_at < $3 AND status IN ('reserved','provider_running','settled','reconciling')`, keyID, starts.Time, ends.Time).Scan(&windowUsed)
		if err != nil {
			return err
		}
		if maximumCharge > temporary.Int64-windowUsed {
			return models.ErrApplicationBudgetExceeded
		}
	}
	return nil
}

// enforceApplicationBudget runs after the billing-account lock and inside the
// serializable reserve transaction. Concurrent reserves for the same owner are
// therefore ordered, and settled operations are counted once rather than as
// both their former reserve and final charge.
func enforceApplicationBudget(ctx context.Context, tx *sql.Tx, applicationID, accountID uuid.UUID, environment string, maximumCharge int64, now time.Time) error {
	const defaultDailyLimit int64 = 1_000_000
	const defaultMonthlyLimit int64 = 10_000_000
	const defaultPerOperationLimit int64 = 250_000
	var daily, monthly, perOperation sql.NullInt64
	var actualAccount uuid.UUID
	var actualEnvironment, status, capabilitiesJSON string
	err := tx.QueryRowContext(ctx, `
		SELECT billing_account_uuid,environment,status,to_json(capabilities)::text,daily_credit_limit,
		       monthly_credit_limit,max_credits_per_operation
		FROM developer_applications WHERE application_uuid=$1 FOR UPDATE
	`, applicationID).Scan(&actualAccount, &actualEnvironment, &status, &capabilitiesJSON, &daily, &monthly, &perOperation)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrInvalidBillingInput
	}
	if err != nil {
		return fmt.Errorf("lock developer application: %w", err)
	}
	var capabilities []string
	if err = json.Unmarshal([]byte(capabilitiesJSON), &capabilities); err != nil {
		return err
	}
	realSandbox := actualEnvironment == "sandbox" && environment == "production" && subset([]string{"ai:real"}, capabilities)
	if actualAccount != accountID || (actualEnvironment != environment && !realSandbox) || status != "active" {
		return models.ErrInvalidBillingInput
	}
	if !daily.Valid {
		daily = sql.NullInt64{Int64: defaultDailyLimit, Valid: true}
	}
	if !monthly.Valid {
		monthly = sql.NullInt64{Int64: defaultMonthlyLimit, Valid: true}
	}
	if !perOperation.Valid {
		perOperation = sql.NullInt64{Int64: defaultPerOperationLimit, Valid: true}
	}
	if perOperation.Valid && maximumCharge > perOperation.Int64 {
		return models.ErrApplicationBudgetExceeded
	}
	dayStart := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	monthStart := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	var dailyUsed, monthlyUsed int64
	err = tx.QueryRowContext(ctx, `
		SELECT
		 COALESCE(sum(CASE WHEN started_at >= $2 THEN CASE WHEN status='settled' THEN settled_credits ELSE reserved_credits END ELSE 0 END),0),
		 COALESCE(sum(CASE WHEN started_at >= $3 THEN CASE WHEN status='settled' THEN settled_credits ELSE reserved_credits END ELSE 0 END),0)
		FROM usage_operations
		WHERE application_uuid=$1 AND status IN ('reserved','provider_running','settled','reconciling')
	`, applicationID, dayStart, monthStart).Scan(&dailyUsed, &monthlyUsed)
	if err != nil {
		return fmt.Errorf("calculate application budget: %w", err)
	}
	if (daily.Valid && maximumCharge > daily.Int64-dailyUsed) || (monthly.Valid && maximumCharge > monthly.Int64-monthlyUsed) {
		return models.ErrApplicationBudgetExceeded
	}
	return nil
}

// SettleCredits charges at most maximum_charge and releases every unused credit.
// Provider overage is recorded as an internal pricing loss, never customer debt.
func (r *Repository) SettleCredits(ctx context.Context, input models.SettleCreditsInput, now time.Time) (models.CreditOperation, error) {
	if input.OperationUUID == uuid.Nil || input.ActualChargeCredits < 0 || input.ProviderCostNanoUSD < 0 {
		return models.CreditOperation{}, models.ErrInvalidBillingInput
	}
	usageJSON := json.RawMessage(input.ProviderUsageJSON)
	if len(usageJSON) == 0 {
		usageJSON = json.RawMessage(`{}`)
	}
	if !json.Valid(usageJSON) {
		return models.CreditOperation{}, models.ErrInvalidBillingInput
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.CreditOperation{}, fmt.Errorf("begin credit settlement: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	operation, environment, err := lockCreditOperation(ctx, tx, input.OperationUUID)
	if err != nil {
		return models.CreditOperation{}, err
	}
	if operation.Status == "settled" {
		return operation, nil
	}
	if operation.Status != "reserved" && operation.Status != "provider_running" {
		return models.CreditOperation{}, models.ErrCreditOperationConflict
	}
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM billing_accounts WHERE billing_account_uuid=$1 FOR UPDATE`, operation.BillingAccountUUID); err != nil {
		return models.CreditOperation{}, fmt.Errorf("lock billing account: %w", err)
	}

	charge := minInt64(input.ActualChargeCredits, operation.MaximumChargeCredits)
	loss := input.ActualChargeCredits - charge
	reservedID, err := operationAccount(ctx, tx, input.OperationUUID, "customer_reserved")
	if err != nil && operation.MaximumChargeCredits > 0 {
		return models.CreditOperation{}, err
	}
	consumedID, err := ensureCustomerAccount(ctx, tx, operation.BillingAccountUUID, environment, "customer_consumed")
	if err != nil {
		return models.CreditOperation{}, err
	}

	allocations, err := reserveAllocations(ctx, tx, input.OperationUUID)
	if err != nil {
		return models.CreditOperation{}, err
	}
	remainingCharge := charge
	settledByGrant := make(map[uuid.UUID]int64, len(allocations))
	for _, allocation := range allocations {
		settled := minInt64(remainingCharge, allocation.reserved)
		settledByGrant[allocation.grantID] = settled
		remainingCharge -= settled
	}
	if remainingCharge != 0 {
		return models.CreditOperation{}, models.ErrCreditOperationConflict
	}

	if charge > 0 {
		if err = createBalancedTransaction(ctx, tx, "settle", "settle:"+input.OperationUUID.String(), input.OperationUUID.String(),
			[]posting{{reservedID, -charge}, {consumedID, charge}}); err != nil {
			return models.CreditOperation{}, err
		}
	}
	release := operation.MaximumChargeCredits - charge
	if release > 0 {
		postings := []posting{{reservedID, -release}}
		for _, allocation := range allocations {
			amount := allocation.reserved - settledByGrant[allocation.grantID]
			if amount == 0 {
				continue
			}
			availableID, accountErr := availableAccountForGrant(ctx, tx, allocation.grantID)
			if accountErr != nil {
				return models.CreditOperation{}, accountErr
			}
			postings = append(postings, posting{availableID, amount})
		}
		if err = createBalancedTransaction(ctx, tx, "release", "release:"+input.OperationUUID.String(), input.OperationUUID.String(), postings); err != nil {
			return models.CreditOperation{}, err
		}
	}
	for _, allocation := range allocations {
		settled := settledByGrant[allocation.grantID]
		if _, err = tx.ExecContext(ctx, `
			UPDATE usage_operation_reservations
			SET settled_credits=$3,released_credits=reserved_credits-$3
			WHERE usage_operation_uuid=$1 AND credit_grant_uuid=$2
		`, input.OperationUUID, allocation.grantID, settled); err != nil {
			return models.CreditOperation{}, fmt.Errorf("settle allocation: %w", err)
		}
	}
	if loss > 0 {
		lossID, accountErr := ensureSystemAccount(ctx, tx, environment, "internal_pricing_loss")
		if accountErr != nil {
			return models.CreditOperation{}, accountErr
		}
		fundingID, accountErr := ensureSystemAccount(ctx, tx, environment, "funding_source")
		if accountErr != nil {
			return models.CreditOperation{}, accountErr
		}
		if err = createBalancedTransaction(ctx, tx, "internal_pricing_loss", "loss:"+input.OperationUUID.String(), input.OperationUUID.String(),
			[]posting{{lossID, loss}, {fundingID, -loss}}); err != nil {
			return models.CreditOperation{}, err
		}
		details, _ := json.Marshal(map[string]any{"actual_charge": input.ActualChargeCredits, "maximum_charge": operation.MaximumChargeCredits, "loss_credits": loss})
		if _, err = tx.ExecContext(ctx, `INSERT INTO billing_alerts(billing_alert_uuid,alert_type,severity,billing_account_uuid,usage_operation_uuid,deduplication_key,details) VALUES(gen_random_uuid(),'internal_pricing_loss','critical',$1,$2,$3,$4) ON CONFLICT(deduplication_key) DO UPDATE SET details=EXCLUDED.details,status='open',resolved_at=NULL`, operation.BillingAccountUUID, input.OperationUUID, "pricing-loss:"+input.OperationUUID.String(), details); err != nil {
			return models.CreditOperation{}, fmt.Errorf("create pricing loss alert: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE usage_operations SET status='settled',settled_credits=$2,
			reserved_credits=0,provider_cost_nano_usd=$3,internal_pricing_loss_credits=$4,
			provider_usage=$5,completed_at=$6
		WHERE usage_operation_uuid=$1
	`, input.OperationUUID, charge, input.ProviderCostNanoUSD, loss, usageJSON, now.UTC()); err != nil {
		return models.CreditOperation{}, fmt.Errorf("complete usage operation: %w", err)
	}
	operation.Status = "settled"
	operation.ReservedCredits = 0
	operation.SettledCredits = charge
	operation.InternalPricingLossCredits = loss
	if err = tx.Commit(); err != nil {
		return models.CreditOperation{}, fmt.Errorf("commit credit settlement: %w", err)
	}
	return operation, nil
}

type allocation struct {
	grantID  uuid.UUID
	reserved int64
}

type posting struct {
	accountID uuid.UUID
	amount    int64
}

func billingAccountForSubscription(ctx context.Context, tx *sql.Tx, subscription models.Subscription) (uuid.UUID, error) {
	var id uuid.UUID
	var err error
	if subscription.UserUUID.Valid {
		err = tx.QueryRowContext(ctx, `SELECT billing_account_uuid FROM billing_accounts WHERE user_uuid=$1`, subscription.UserUUID.UUID).Scan(&id)
	} else if subscription.CompanyUUID.Valid {
		err = tx.QueryRowContext(ctx, `SELECT billing_account_uuid FROM billing_accounts WHERE company_uuid=$1`, subscription.CompanyUUID.UUID).Scan(&id)
	} else {
		return uuid.Nil, models.ErrInvalidBillingInput
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("get billing account: %w", err)
	}
	return id, nil
}

func activePricingCatalog(ctx context.Context, tx *sql.Tx, now time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRowContext(ctx, `
		SELECT pricing_catalog_version_uuid FROM pricing_catalog_versions
		WHERE status='active' AND effective_from <= $1 AND (effective_until IS NULL OR effective_until > $1)
	`, now.UTC()).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("get active pricing catalog: %w", err)
	}
	return id, nil
}

func spendableGrants(ctx context.Context, tx *sql.Tx, accountID, applicationID uuid.UUID, environment string, now time.Time) ([]grantBalance, int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT g.credit_grant_uuid,a.credit_ledger_account_uuid,COALESCE(sum(p.amount_credits),0) AS available
		FROM credit_grants g
		JOIN credit_ledger_accounts a ON a.credit_grant_uuid=g.credit_grant_uuid AND a.account_type='customer_available'
		LEFT JOIN credit_ledger_postings p ON p.credit_ledger_account_uuid=a.credit_ledger_account_uuid
		WHERE g.billing_account_uuid=$1 AND g.environment=$2 AND (g.expires_at IS NULL OR g.expires_at>$3)
		  AND (($2='sandbox' AND g.application_uuid=$4) OR ($2='production' AND g.application_uuid IS NULL))
		GROUP BY g.credit_grant_uuid,a.credit_ledger_account_uuid,g.grant_type,g.expires_at,g.created_at
		HAVING COALESCE(sum(p.amount_credits),0)>0
		ORDER BY CASE g.grant_type WHEN 'subscription_allowance' THEN 1 WHEN 'promotional' THEN 2 WHEN 'purchased' THEN 3 ELSE 4 END,
		         g.expires_at NULLS LAST,g.created_at
	`, accountID, environment, now.UTC(), applicationID)
	if err != nil {
		return nil, 0, fmt.Errorf("list spendable grants: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result []grantBalance
	var total int64
	for rows.Next() {
		var item grantBalance
		if err = rows.Scan(&item.grantID, &item.accountID, &item.available); err != nil {
			return nil, 0, fmt.Errorf("scan spendable grant: %w", err)
		}
		result = append(result, item)
		total += item.available
	}
	return result, total, rows.Err()
}

func findCreditOperationByIdempotency(ctx context.Context, tx *sql.Tx, accountID uuid.UUID, environment, key string) (models.CreditOperation, bool, error) {
	var result models.CreditOperation
	err := tx.QueryRowContext(ctx, `
		SELECT usage_operation_uuid,billing_account_uuid,status,maximum_charge_credits,reserved_credits,settled_credits,internal_pricing_loss_credits
		FROM usage_operations WHERE billing_account_uuid=$1 AND environment=$2 AND idempotency_key=$3
	`, accountID, environment, key).Scan(&result.ID, &result.BillingAccountUUID, &result.Status,
		&result.MaximumChargeCredits, &result.ReservedCredits, &result.SettledCredits, &result.InternalPricingLossCredits)
	if errors.Is(err, sql.ErrNoRows) {
		return models.CreditOperation{}, false, nil
	}
	if err != nil {
		return models.CreditOperation{}, false, fmt.Errorf("find usage operation: %w", err)
	}
	return result, true, nil
}

func lockCreditOperation(ctx context.Context, tx *sql.Tx, id uuid.UUID) (models.CreditOperation, string, error) {
	var result models.CreditOperation
	var environment string
	err := tx.QueryRowContext(ctx, `
		SELECT usage_operation_uuid,billing_account_uuid,status,environment,maximum_charge_credits,reserved_credits,settled_credits,internal_pricing_loss_credits
		FROM usage_operations WHERE usage_operation_uuid=$1 FOR UPDATE
	`, id).Scan(&result.ID, &result.BillingAccountUUID, &result.Status, &environment,
		&result.MaximumChargeCredits, &result.ReservedCredits, &result.SettledCredits, &result.InternalPricingLossCredits)
	if errors.Is(err, sql.ErrNoRows) {
		return models.CreditOperation{}, "", models.ErrCreditOperationNotFound
	}
	if err != nil {
		return models.CreditOperation{}, "", fmt.Errorf("lock usage operation: %w", err)
	}
	return result, environment, nil
}

func ensureOperationAccount(ctx context.Context, tx *sql.Tx, accountID, operationID uuid.UUID, environment, accountType string) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,billing_account_uuid,environment,account_type,operation_uuid)
		VALUES($1,$2,$3,$4,$5)
		ON CONFLICT (operation_uuid,account_type) WHERE account_type='customer_reserved'
		DO UPDATE SET operation_uuid=EXCLUDED.operation_uuid
		RETURNING credit_ledger_account_uuid
	`, id, accountID, environment, accountType, operationID).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ensure operation account: %w", err)
	}
	return id, nil
}

func operationAccount(ctx context.Context, tx *sql.Tx, operationID uuid.UUID, accountType string) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRowContext(ctx, `SELECT credit_ledger_account_uuid FROM credit_ledger_accounts WHERE operation_uuid=$1 AND account_type=$2`, operationID, accountType).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("get operation ledger account: %w", err)
	}
	return id, nil
}

func ensureCustomerAccount(ctx context.Context, tx *sql.Tx, accountID uuid.UUID, environment, accountType string) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,billing_account_uuid,environment,account_type)
		VALUES($1,$2,$3,$4)
		ON CONFLICT (billing_account_uuid,environment,account_type) WHERE account_type='customer_consumed'
		DO UPDATE SET billing_account_uuid=EXCLUDED.billing_account_uuid
		RETURNING credit_ledger_account_uuid
	`, id, accountID, environment, accountType).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ensure customer ledger account: %w", err)
	}
	return id, nil
}

func ensureSystemAccount(ctx context.Context, tx *sql.Tx, environment, accountType string) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, err
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,environment,account_type)
		VALUES($1,$2,$3)
		ON CONFLICT (environment,account_type)
		WHERE billing_account_uuid IS NULL AND credit_grant_uuid IS NULL AND operation_uuid IS NULL
		DO UPDATE SET environment=EXCLUDED.environment
		RETURNING credit_ledger_account_uuid
	`, id, environment, accountType).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("ensure system ledger account: %w", err)
	}
	return id, nil
}

func reserveAllocations(ctx context.Context, tx *sql.Tx, operationID uuid.UUID) ([]allocation, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT credit_grant_uuid,reserved_credits FROM usage_operation_reservations
		WHERE usage_operation_uuid=$1 ORDER BY spend_order FOR UPDATE
	`, operationID)
	if err != nil {
		return nil, fmt.Errorf("list reserve allocations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result []allocation
	for rows.Next() {
		var item allocation
		if err = rows.Scan(&item.grantID, &item.reserved); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func availableAccountForGrant(ctx context.Context, tx *sql.Tx, grantID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRowContext(ctx, `SELECT credit_ledger_account_uuid FROM credit_ledger_accounts WHERE credit_grant_uuid=$1 AND account_type='customer_available'`, grantID).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("get available ledger account: %w", err)
	}
	return id, nil
}

func createBalancedTransaction(ctx context.Context, tx *sql.Tx, transactionType, key, reference string, postings []posting) error {
	if len(postings) < 2 {
		return models.ErrCreditOperationConflict
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO credit_ledger_transactions(
			credit_ledger_transaction_uuid,transaction_type,idempotency_key,actor_type,reason,source_reference
		) VALUES($1,$2,$3,'system',$2,$4)
	`, id, transactionType, key, reference); err != nil {
		return fmt.Errorf("create %s transaction: %w", transactionType, err)
	}
	for _, item := range postings {
		if item.amount == 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO credit_ledger_postings(
				credit_ledger_posting_uuid,credit_ledger_transaction_uuid,credit_ledger_account_uuid,amount_credits
			) VALUES(gen_random_uuid(),$1,$2,$3)
		`, id, item.accountID, item.amount); err != nil {
			return fmt.Errorf("post %s transaction: %w", transactionType, err)
		}
	}
	return nil
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
