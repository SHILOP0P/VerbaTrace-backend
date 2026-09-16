package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) GetSandboxWallet(ctx context.Context, appID, actorID uuid.UUID) (models.SandboxWalletDashboard, error) {
	var result models.SandboxWalletDashboard
	result.ApplicationUUID = appID
	err := r.db.QueryRowContext(ctx, `SELECT a.name,COALESCE(sum(p.amount_credits),0)
		FROM developer_applications a
		LEFT JOIN credit_grants g ON g.application_uuid=a.application_uuid
		LEFT JOIN credit_ledger_accounts la ON la.credit_grant_uuid=g.credit_grant_uuid AND la.account_type='customer_available'
		LEFT JOIN credit_ledger_postings p ON p.credit_ledger_account_uuid=la.credit_ledger_account_uuid
		WHERE a.application_uuid=$1 AND a.environment='sandbox' AND a.status<>'revoked' AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies co WHERE co.company_uuid=a.company_uuid AND co.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role IN ('company_manager','company_deputy')))))
		GROUP BY a.application_uuid,a.name`, appID, actorID).Scan(&result.ApplicationName, &result.BalanceCredits)
	if errors.Is(err, sql.ErrNoRows) {
		return result, models.ErrForbidden
	}
	if err != nil {
		return result, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT t.credit_ledger_transaction_uuid,t.transaction_type,COALESCE(sum(p.amount_credits),0),t.reason,t.created_at
		FROM credit_ledger_transactions t
		JOIN credit_ledger_postings p ON p.credit_ledger_transaction_uuid=t.credit_ledger_transaction_uuid
		JOIN credit_ledger_accounts la ON la.credit_ledger_account_uuid=p.credit_ledger_account_uuid
		JOIN credit_grants g ON g.credit_grant_uuid=la.credit_grant_uuid
		WHERE g.application_uuid=$1 AND la.account_type='customer_available'
		GROUP BY t.credit_ledger_transaction_uuid,t.transaction_type,t.reason,t.created_at
		ORDER BY t.created_at DESC LIMIT 50`, appID)
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	result.Entries = make([]models.CreditWalletEntry, 0)
	for rows.Next() {
		var entry models.CreditWalletEntry
		var created time.Time
		if err = rows.Scan(&entry.TransactionUUID, &entry.Type, &entry.Credits, &entry.Reason, &created); err != nil {
			return result, err
		}
		entry.CreatedAt = created
		result.Entries = append(result.Entries, entry)
	}
	return result, rows.Err()
}

const (
	defaultSandboxBalance int64 = 100_000
	maxSandboxAdjustment  int64 = 1_000_000
	maxSandboxBalance     int64 = 10_000_000
)

func (r *Repository) AdjustSandboxWallet(ctx context.Context, appID, actorID uuid.UUID, mode string, credits int64, requestID string) (int64, error) {
	requestID = strings.TrimSpace(requestID)
	if appID == uuid.Nil || actorID == uuid.Nil || requestID == "" || len(requestID) > 200 {
		return 0, models.ErrInvalidBillingInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var accountID uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT a.billing_account_uuid FROM developer_applications a WHERE a.application_uuid=$1 AND a.environment='sandbox' AND a.status='active' AND ((a.owner_type='user' AND a.user_uuid=$2) OR (a.owner_type='company' AND (EXISTS(SELECT 1 FROM companies co WHERE co.company_uuid=a.company_uuid AND co.manager_user_uuid=$2) OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$2 AND cm.status='active' AND cm.role IN ('company_manager','company_deputy')))))`, appID, actorID).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, models.ErrForbidden
	}
	if err != nil {
		return 0, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT billing_account_uuid FROM billing_accounts WHERE billing_account_uuid=$1 FOR UPDATE`, accountID).Scan(&accountID); err != nil {
		return 0, err
	}
	var lockedApp uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT application_uuid FROM developer_applications WHERE application_uuid=$1 AND status='active' FOR UPDATE`, appID).Scan(&lockedApp); err != nil {
		return 0, models.ErrForbidden
	}
	current, err := sandboxBalance(ctx, tx, appID)
	if err != nil {
		return 0, err
	}
	idempotency := "sandbox-wallet:" + appID.String() + ":" + requestID
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM credit_ledger_transactions WHERE idempotency_key=$1)`, idempotency).Scan(&exists); err != nil {
		return 0, err
	}
	if exists {
		return current, tx.Commit()
	}
	var target int64
	switch mode {
	case "add":
		if credits <= 0 || credits > maxSandboxAdjustment {
			return 0, models.ErrInvalidBillingInput
		}
		target = current + credits
	case "set":
		if credits < 0 || credits > maxSandboxBalance {
			return 0, models.ErrInvalidBillingInput
		}
		target = credits
	case "reset":
		target = defaultSandboxBalance
	default:
		return 0, models.ErrInvalidBillingInput
	}
	if target < 0 || target > maxSandboxBalance {
		return 0, models.ErrInvalidBillingInput
	}
	delta := target - current
	if delta == 0 {
		transactionID, _ := uuid.NewV7()
		_, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger_transactions(credit_ledger_transaction_uuid,transaction_type,idempotency_key,actor_type,actor_uuid,reason,source_reference) VALUES($1,'adjustment',$2,'user',$3,'sandbox wallet no-op',$4)`, transactionID, idempotency, actorID, appID.String())
		if err != nil {
			return 0, err
		}
		return target, tx.Commit()
	}
	fundingID, err := ensureSystemAccount(ctx, tx, "sandbox", "funding_source")
	if err != nil {
		return 0, err
	}
	transactionID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger_transactions(credit_ledger_transaction_uuid,transaction_type,idempotency_key,actor_type,actor_uuid,reason,source_reference) VALUES($1,'adjustment',$2,'user',$3,$4,$5)`, transactionID, idempotency, actorID, "sandbox wallet "+mode, appID.String())
	if err != nil {
		return 0, err
	}
	if delta > 0 {
		grantID, _ := uuid.NewV7()
		availableID, _ := uuid.NewV7()
		reference := "sandbox-adjustment:" + appID.String() + ":" + requestID
		if _, err = tx.ExecContext(ctx, `INSERT INTO credit_grants(credit_grant_uuid,billing_account_uuid,application_uuid,grant_type,environment,original_credits,source_reference) VALUES($1,$2,$3,'sandbox','sandbox',$4,$5)`, grantID, accountID, appID, delta, reference); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,billing_account_uuid,credit_grant_uuid,environment,account_type) VALUES($1,$2,$3,'sandbox','customer_available')`, availableID, accountID, grantID); err != nil {
			return 0, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger_postings(credit_ledger_posting_uuid,credit_ledger_transaction_uuid,credit_ledger_account_uuid,amount_credits) VALUES(gen_random_uuid(),$1,$2,$4::bigint),(gen_random_uuid(),$1,$3,-($4::bigint))`, transactionID, availableID, fundingID, delta)
	} else {
		remaining := -delta
		rows, qErr := tx.QueryContext(ctx, `SELECT a.credit_ledger_account_uuid,COALESCE(sum(p.amount_credits),0) FROM credit_grants g JOIN credit_ledger_accounts a ON a.credit_grant_uuid=g.credit_grant_uuid AND a.account_type='customer_available' LEFT JOIN credit_ledger_postings p USING(credit_ledger_account_uuid) WHERE g.application_uuid=$1 GROUP BY a.credit_ledger_account_uuid,g.created_at HAVING COALESCE(sum(p.amount_credits),0)>0 ORDER BY g.created_at`, appID)
		if qErr != nil {
			return 0, qErr
		}
		type balance struct {
			id     uuid.UUID
			amount int64
		}
		var balances []balance
		for rows.Next() {
			var b balance
			if qErr = rows.Scan(&b.id, &b.amount); qErr != nil {
				_ = rows.Close()
				return 0, qErr
			}
			balances = append(balances, b)
		}
		qErr = rows.Close()
		if qErr != nil {
			return 0, qErr
		}
		for _, b := range balances {
			if remaining == 0 {
				break
			}
			take := b.amount
			if take > remaining {
				take = remaining
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger_postings(credit_ledger_posting_uuid,credit_ledger_transaction_uuid,credit_ledger_account_uuid,amount_credits) VALUES(gen_random_uuid(),$1,$2,-($4::bigint)),(gen_random_uuid(),$1,$3,$4::bigint)`, transactionID, b.id, fundingID, take); err != nil {
				return 0, err
			}
			remaining -= take
		}
		if remaining != 0 {
			return 0, fmt.Errorf("sandbox wallet invariant: missing %d credits", remaining)
		}
	}
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return target, nil
}

func sandboxBalance(ctx context.Context, tx *sql.Tx, appID uuid.UUID) (int64, error) {
	var value int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(p.amount_credits),0) FROM credit_grants g JOIN credit_ledger_accounts a ON a.credit_grant_uuid=g.credit_grant_uuid AND a.account_type='customer_available' LEFT JOIN credit_ledger_postings p USING(credit_ledger_account_uuid) WHERE g.application_uuid=$1`, appID).Scan(&value)
	return value, err
}
