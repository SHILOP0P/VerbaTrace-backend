package billing

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// MockPurchaseCredits exercises the complete wallet ledger without charging a
// payment provider. Reusing RequestID is idempotent.
func (r *Repository) MockPurchaseCredits(ctx context.Context, input models.MockCreditPurchaseInput) (int64, error) {
	if input.OwnerUUID == uuid.Nil || input.ActorUUID == uuid.Nil || input.Credits <= 0 || input.Credits > 10_000_000 || input.RequestID == "" {
		return 0, models.ErrInvalidBillingInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	accountID, _ := uuid.NewV7()
	switch input.OwnerType {
	case "user":
		if err = tx.QueryRowContext(ctx, `INSERT INTO billing_accounts(billing_account_uuid,owner_type,user_uuid) VALUES($1,'user',$2) ON CONFLICT(user_uuid) WHERE user_uuid IS NOT NULL DO UPDATE SET updated_at=now() RETURNING billing_account_uuid`, accountID, input.OwnerUUID).Scan(&accountID); err != nil {
			return 0, err
		}
	case "company":
		if err = tx.QueryRowContext(ctx, `INSERT INTO billing_accounts(billing_account_uuid,owner_type,company_uuid) VALUES($1,'company',$2) ON CONFLICT(company_uuid) WHERE company_uuid IS NOT NULL DO UPDATE SET updated_at=now() RETURNING billing_account_uuid`, accountID, input.OwnerUUID).Scan(&accountID); err != nil {
			return 0, err
		}
	default:
		return 0, models.ErrInvalidBillingInput
	}
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM billing_accounts WHERE billing_account_uuid=$1 FOR UPDATE`, accountID); err != nil {
		return 0, err
	}
	key := "mock-purchase:" + input.RequestID
	var existing int64
	err = tx.QueryRowContext(ctx, `SELECT g.original_credits FROM credit_grants g JOIN credit_ledger_transactions t ON t.source_reference=g.source_reference WHERE g.billing_account_uuid=$1 AND t.idempotency_key=$2`, accountID, key).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	grantID, _ := uuid.NewV7()
	availableID, _ := uuid.NewV7()
	fundingID, _ := uuid.NewV7()
	tID, _ := uuid.NewV7()
	reference := "mock-purchase:" + grantID.String()
	if _, err = tx.ExecContext(ctx, `INSERT INTO credit_grants(credit_grant_uuid,billing_account_uuid,grant_type,environment,original_credits,source_reference) VALUES($1,$2,'purchased','production',$3,$4)`, grantID, accountID, input.Credits, reference); err != nil {
		return 0, fmt.Errorf("create purchased grant: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,billing_account_uuid,credit_grant_uuid,environment,account_type) VALUES($1,$2,$3,'production','customer_available')`, availableID, accountID, grantID); err != nil {
		return 0, err
	}
	if err = tx.QueryRowContext(ctx, `INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,environment,account_type) VALUES($1,'production','funding_source') ON CONFLICT(environment,account_type) WHERE billing_account_uuid IS NULL AND credit_grant_uuid IS NULL AND operation_uuid IS NULL DO UPDATE SET environment=EXCLUDED.environment RETURNING credit_ledger_account_uuid`, fundingID).Scan(&fundingID); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger_transactions(credit_ledger_transaction_uuid,transaction_type,idempotency_key,actor_type,actor_uuid,reason,source_reference,created_at) VALUES($1,'purchase',$2,'user',$3,'mock payment checkout',$4,$5)`, tID, key, input.ActorUUID, reference, time.Now().UTC()); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger_postings(credit_ledger_posting_uuid,credit_ledger_transaction_uuid,credit_ledger_account_uuid,amount_credits) VALUES(gen_random_uuid(),$1,$2,$4::bigint),(gen_random_uuid(),$1,$3,-($4::bigint))`, tID, availableID, fundingID, input.Credits); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return input.Credits, nil
}
