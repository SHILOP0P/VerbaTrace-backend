package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// EnsureCurrentCreditUsage lazily opens the calendar-month allowance. The whole
// grant and its two double-entry postings are committed atomically.
func (r *Repository) EnsureCurrentCreditUsage(ctx context.Context, subscription models.Subscription, now time.Time) (models.CreditUsage, error) {
	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		usage, err := r.ensureCurrentCreditUsageOnce(ctx, subscription, now)
		if err == nil {
			return usage, nil
		}
		lastErr = err
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "40001" && pgErr.Code != "40P01") {
			return models.CreditUsage{}, err
		}
		select {
		case <-ctx.Done():
			return models.CreditUsage{}, ctx.Err()
		case <-time.After((10 * time.Millisecond) << attempt):
		}
	}
	return models.CreditUsage{}, fmt.Errorf("ensure current credit usage retries exhausted: %w", lastErr)
}

func (r *Repository) ensureCurrentCreditUsageOnce(ctx context.Context, subscription models.Subscription, now time.Time) (models.CreditUsage, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.CreditUsage{}, fmt.Errorf("begin credit usage: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	accountID, err := ensureBillingAccount(ctx, tx, subscription)
	if err != nil {
		return models.CreditUsage{}, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM billing_accounts WHERE billing_account_uuid=$1 FOR UPDATE`, accountID); err != nil {
		return models.CreditUsage{}, fmt.Errorf("lock billing account: %w", err)
	}

	start := monthStart(now)
	end := start.AddDate(0, 1, 0)
	if _, err = tx.ExecContext(ctx, `
		UPDATE allowance_epochs SET status='closed',closed_at=$2
		WHERE subscription_uuid=$1 AND status='open' AND ends_at <= $2
	`, subscription.ID, now.UTC()); err != nil {
		return models.CreditUsage{}, fmt.Errorf("close expired allowance: %w", err)
	}

	epochID, created, err := ensureAllowanceEpoch(ctx, tx, subscription, accountID, start, end)
	if err != nil {
		return models.CreditUsage{}, err
	}
	if created {
		if err = createAllowanceGrant(ctx, tx, accountID, epochID, subscription.Plan.MonthlyCreditAllowance, start); err != nil {
			return models.CreditUsage{}, err
		}
	}

	usage, err := readCreditUsage(ctx, tx, accountID, epochID, subscription.Plan.MonthlyCreditAllowance, end)
	if err != nil {
		return models.CreditUsage{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.CreditUsage{}, fmt.Errorf("commit credit usage: %w", err)
	}
	return usage, nil
}

func ensureBillingAccount(ctx context.Context, tx *sql.Tx, subscription models.Subscription) (uuid.UUID, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generate billing account uuid: %w", err)
	}
	var row *sql.Row
	switch {
	case subscription.UserUUID.Valid:
		row = tx.QueryRowContext(ctx, `
			INSERT INTO billing_accounts(billing_account_uuid,owner_type,user_uuid)
			VALUES($1,'user',$2)
			ON CONFLICT (user_uuid) WHERE user_uuid IS NOT NULL
			DO UPDATE SET updated_at=billing_accounts.updated_at
			RETURNING billing_account_uuid
		`, id, subscription.UserUUID.UUID)
	case subscription.CompanyUUID.Valid:
		row = tx.QueryRowContext(ctx, `
			INSERT INTO billing_accounts(billing_account_uuid,owner_type,company_uuid)
			VALUES($1,'company',$2)
			ON CONFLICT (company_uuid) WHERE company_uuid IS NOT NULL
			DO UPDATE SET updated_at=billing_accounts.updated_at
			RETURNING billing_account_uuid
		`, id, subscription.CompanyUUID.UUID)
	default:
		return uuid.Nil, models.ErrInvalidBillingInput
	}
	if err = row.Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("ensure billing account: %w", err)
	}
	return id, nil
}

func ensureAllowanceEpoch(ctx context.Context, tx *sql.Tx, subscription models.Subscription, accountID uuid.UUID, start, end time.Time) (uuid.UUID, bool, error) {
	var existing uuid.UUID
	err := tx.QueryRowContext(ctx, `
		SELECT allowance_epoch_uuid FROM allowance_epochs
		WHERE subscription_uuid=$1 AND status='open' AND ends_at>$2 AND starts_at<$3
		ORDER BY starts_at DESC LIMIT 1
	`, subscription.ID, start, end).Scan(&existing)
	if err == nil {
		return existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, false, fmt.Errorf("find allowance epoch: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("generate allowance epoch uuid: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO allowance_epochs(
			allowance_epoch_uuid,billing_account_uuid,subscription_uuid,starts_at,ends_at,
			allowance_credits,status
		) VALUES($1,$2,$3,$4,$5,$6,'open')
	`, id, accountID, subscription.ID, start, end, subscription.Plan.MonthlyCreditAllowance)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("create allowance epoch: %w", err)
	}
	return id, true, nil
}

func createAllowanceGrant(ctx context.Context, tx *sql.Tx, accountID, epochID uuid.UUID, credits int64, start time.Time) error {
	grantID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate allowance grant uuid: %w", err)
	}
	reference := "allowance:" + epochID.String()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO credit_grants(
			credit_grant_uuid,billing_account_uuid,allowance_epoch_uuid,grant_type,
			environment,original_credits,expires_at,source_reference
		) VALUES($1,$2,$3,'subscription_allowance','production',$4,$5,$6)
	`, grantID, accountID, epochID, credits, start.AddDate(0, 1, 0), reference); err != nil {
		return fmt.Errorf("create allowance grant: %w", err)
	}

	availableID, fundingID, err := ensureGrantAccounts(ctx, tx, accountID, grantID, "production")
	if err != nil {
		return err
	}
	if credits == 0 {
		// A zero allowance is represented by the epoch/grant and needs no ledger transaction.
		return nil
	}
	transactionID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate allowance transaction uuid: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO credit_ledger_transactions(
			credit_ledger_transaction_uuid,transaction_type,idempotency_key,actor_type,reason,source_reference
		) VALUES($1,'allowance_opened',$2,'system','monthly allowance opened',$3)
	`, transactionID, reference, reference); err != nil {
		return fmt.Errorf("create allowance transaction: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO credit_ledger_postings(
			credit_ledger_posting_uuid,credit_ledger_transaction_uuid,credit_ledger_account_uuid,amount_credits
		) VALUES(gen_random_uuid(),$1,$2,$4),
		        (gen_random_uuid(),$1,$3,-$4)
	`, transactionID, availableID, fundingID, credits); err != nil {
		return fmt.Errorf("post allowance transaction: %w", err)
	}
	return nil
}

func ensureGrantAccounts(ctx context.Context, tx *sql.Tx, accountID, grantID uuid.UUID, environment string) (uuid.UUID, uuid.UUID, error) {
	availableID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err = tx.QueryRowContext(ctx, `
		INSERT INTO credit_ledger_accounts(
			credit_ledger_account_uuid,billing_account_uuid,credit_grant_uuid,environment,account_type
		) VALUES($1,$2,$3,$4,'customer_available')
		ON CONFLICT (credit_grant_uuid,account_type) WHERE account_type='customer_available'
		DO UPDATE SET credit_grant_uuid=EXCLUDED.credit_grant_uuid
		RETURNING credit_ledger_account_uuid
	`, availableID, accountID, grantID, environment).Scan(&availableID); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("ensure available ledger account: %w", err)
	}

	fundingID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err = tx.QueryRowContext(ctx, `
		INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,environment,account_type)
		VALUES($1,$2,'funding_source')
		ON CONFLICT (environment,account_type)
		WHERE billing_account_uuid IS NULL AND credit_grant_uuid IS NULL AND operation_uuid IS NULL
		DO UPDATE SET environment=EXCLUDED.environment
		RETURNING credit_ledger_account_uuid
	`, fundingID, environment).Scan(&fundingID); err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("ensure funding ledger account: %w", err)
	}
	return availableID, fundingID, nil
}

func readCreditUsage(ctx context.Context, tx *sql.Tx, accountID, epochID uuid.UUID, allowance int64, resetsAt time.Time) (models.CreditUsage, error) {
	var remaining, wallet int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(sum(p.amount_credits),0)
		FROM credit_ledger_postings p
		JOIN credit_ledger_accounts a ON a.credit_ledger_account_uuid=p.credit_ledger_account_uuid
		JOIN credit_grants g ON g.credit_grant_uuid=a.credit_grant_uuid
		WHERE a.account_type='customer_available' AND g.allowance_epoch_uuid=$1
	`, epochID).Scan(&remaining); err != nil {
		return models.CreditUsage{}, fmt.Errorf("read allowance balance: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(sum(p.amount_credits),0)
		FROM credit_ledger_postings p
		JOIN credit_ledger_accounts a ON a.credit_ledger_account_uuid=p.credit_ledger_account_uuid
		JOIN credit_grants g ON g.credit_grant_uuid=a.credit_grant_uuid
		WHERE a.account_type='customer_available' AND g.billing_account_uuid=$1
		  AND g.environment='production' AND g.grant_type IN ('purchased','promotional')
		  AND (g.expires_at IS NULL OR g.expires_at > now())
	`, accountID).Scan(&wallet); err != nil {
		return models.CreditUsage{}, fmt.Errorf("read wallet balance: %w", err)
	}
	return models.CreditUsage{AllowanceCredits: allowance, AllowanceRemaining: remaining, WalletCredits: wallet, ResetsAt: resetsAt}, nil
}
