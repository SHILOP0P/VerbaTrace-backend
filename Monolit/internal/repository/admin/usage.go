package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// ResetAdminUsage replaces only the current subscription allowance. Purchased
// and promotional wallet grants are never selected or modified.
func (r *Repository) ResetAdminUsage(ctx context.Context, input models.ResetAdminUsageInput) error {
	if input.ActorUserUUID == uuid.Nil || (input.UserUUID == uuid.Nil) == (input.CompanyUUID == uuid.Nil) || len(input.Metadata.Reason) < 3 {
		return models.ErrInvalidAdminInput
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	actor, err := getAdminUserForUpdate(ctx, tx, input.ActorUserUUID)
	if err != nil {
		return err
	}
	if actor.Role != models.UserRoleSuperAdmin {
		return models.ErrForbidden
	}
	ownerColumn, owner, subjectType := "user_uuid", input.UserUUID, "user"
	if input.CompanyUUID != uuid.Nil {
		ownerColumn, owner, subjectType = "company_uuid", input.CompanyUUID, "company"
	}
	var subscriptionID uuid.UUID
	var allowance int64
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT s.subscription_uuid,p.monthly_credit_allowance FROM subscriptions s JOIN plans p USING(plan_uuid) WHERE s.%s=$1 AND s.status='active' ORDER BY s.starts_at DESC LIMIT 1 FOR UPDATE OF s`, ownerColumn), owner).Scan(&subscriptionID, &allowance)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrSubscriptionNotFound
	}
	if err != nil {
		return err
	}
	accountID, _ := uuid.NewV7()
	if subjectType == "user" {
		err = tx.QueryRowContext(ctx, `INSERT INTO billing_accounts(billing_account_uuid,owner_type,user_uuid) VALUES($1,'user',$2) ON CONFLICT(user_uuid) WHERE user_uuid IS NOT NULL DO UPDATE SET updated_at=now() RETURNING billing_account_uuid`, accountID, owner).Scan(&accountID)
	} else {
		err = tx.QueryRowContext(ctx, `INSERT INTO billing_accounts(billing_account_uuid,owner_type,company_uuid) VALUES($1,'company',$2) ON CONFLICT(company_uuid) WHERE company_uuid IS NOT NULL DO UPDATE SET updated_at=now() RETURNING billing_account_uuid`, accountID, owner).Scan(&accountID)
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM billing_accounts WHERE billing_account_uuid=$1 FOR UPDATE`, accountID); err != nil {
		return err
	}
	now := time.Now().UTC()
	monthEnd := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	var previous uuid.NullUUID
	err = tx.QueryRowContext(ctx, `SELECT allowance_epoch_uuid FROM allowance_epochs WHERE subscription_uuid=$1 AND status='open' FOR UPDATE`, subscriptionID).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previous.Valid {
		if err = closeAllowance(ctx, tx, previous.UUID, actor.ID, input.Metadata.Reason); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE allowance_epochs SET status='reset',reset_by_user_uuid=$2,reset_reason=$3,closed_at=$4 WHERE allowance_epoch_uuid=$1`, previous.UUID, actor.ID, input.Metadata.Reason, now); err != nil {
			return err
		}
	}
	epochID, _ := uuid.NewV7()
	if _, err = tx.ExecContext(ctx, `INSERT INTO allowance_epochs(allowance_epoch_uuid,billing_account_uuid,subscription_uuid,starts_at,ends_at,allowance_credits,status) VALUES($1,$2,$3,$4,$5,$6,'open')`, epochID, accountID, subscriptionID, now, monthEnd, allowance); err != nil {
		return err
	}
	if err = openAllowance(ctx, tx, accountID, epochID, allowance, actor.ID, input.Metadata.Reason, monthEnd); err != nil {
		return err
	}
	after, _ := json.Marshal(map[string]any{"owner_uuid": owner, "allowance_credits": allowance, "new_epoch_uuid": epochID, "wallet_changed": false})
	if err = insertAudit(ctx, tx, models.AdminAuditLog{ID: mustUUIDv7(), ActorUserUUID: actor.ID, ActorRole: actor.Role, Action: "credit_allowance.reset", TargetType: subjectType, TargetUUID: uuid.NullUUID{UUID: owner, Valid: true}, AfterData: after, Reason: &input.Metadata.Reason, RequestID: input.Metadata.RequestID, IPAddress: input.Metadata.IPAddress, UserAgent: input.Metadata.UserAgent, CreatedAt: now}); err != nil {
		return err
	}
	return tx.Commit()
}

func closeAllowance(ctx context.Context, tx *sql.Tx, epochID, actorID uuid.UUID, reason string) error {
	var availableID uuid.UUID
	var balance int64
	err := tx.QueryRowContext(ctx, `SELECT a.credit_ledger_account_uuid,COALESCE(sum(p.amount_credits),0) FROM credit_grants g JOIN credit_ledger_accounts a ON a.credit_grant_uuid=g.credit_grant_uuid AND a.account_type='customer_available' LEFT JOIN credit_ledger_postings p ON p.credit_ledger_account_uuid=a.credit_ledger_account_uuid WHERE g.allowance_epoch_uuid=$1 GROUP BY a.credit_ledger_account_uuid`, epochID).Scan(&availableID, &balance)
	if errors.Is(err, sql.ErrNoRows) || balance == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	expiryID, _ := uuid.NewV7()
	if err = tx.QueryRowContext(ctx, `INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,environment,account_type) VALUES($1,'production','expiry_source') ON CONFLICT(environment,account_type) WHERE billing_account_uuid IS NULL AND credit_grant_uuid IS NULL AND operation_uuid IS NULL DO UPDATE SET environment=EXCLUDED.environment RETURNING credit_ledger_account_uuid`, expiryID).Scan(&expiryID); err != nil {
		return err
	}
	tID, _ := uuid.NewV7()
	if _, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger_transactions(credit_ledger_transaction_uuid,transaction_type,idempotency_key,actor_type,actor_uuid,reason,source_reference) VALUES($1,'allowance_closed',$2,'admin',$3,$4,$5)`, tID, "allowance-close:"+epochID.String(), actorID, reason, epochID.String()); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger_postings(credit_ledger_posting_uuid,credit_ledger_transaction_uuid,credit_ledger_account_uuid,amount_credits) VALUES(gen_random_uuid(),$1,$2,-($4::bigint)),(gen_random_uuid(),$1,$3,$4::bigint)`, tID, availableID, expiryID, balance)
	return err
}

func openAllowance(ctx context.Context, tx *sql.Tx, accountID, epochID uuid.UUID, credits int64, actorID uuid.UUID, reason string, expires time.Time) error {
	grantID, _ := uuid.NewV7()
	availableID, _ := uuid.NewV7()
	fundingID, _ := uuid.NewV7()
	reference := "allowance:" + epochID.String()
	if _, err := tx.ExecContext(ctx, `INSERT INTO credit_grants(credit_grant_uuid,billing_account_uuid,allowance_epoch_uuid,grant_type,environment,original_credits,expires_at,source_reference) VALUES($1,$2,$3,'subscription_allowance','production',$4,$5,$6)`, grantID, accountID, epochID, credits, expires, reference); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,billing_account_uuid,credit_grant_uuid,environment,account_type) VALUES($1,$2,$3,'production','customer_available')`, availableID, accountID, grantID); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO credit_ledger_accounts(credit_ledger_account_uuid,environment,account_type) VALUES($1,'production','funding_source') ON CONFLICT(environment,account_type) WHERE billing_account_uuid IS NULL AND credit_grant_uuid IS NULL AND operation_uuid IS NULL DO UPDATE SET environment=EXCLUDED.environment RETURNING credit_ledger_account_uuid`, fundingID).Scan(&fundingID); err != nil {
		return err
	}
	if credits == 0 {
		return nil
	}
	tID, _ := uuid.NewV7()
	if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger_transactions(credit_ledger_transaction_uuid,transaction_type,idempotency_key,actor_type,actor_uuid,reason,source_reference) VALUES($1,'allowance_reset',$2,'admin',$3,$4,$5)`, tID, "allowance-reset:"+epochID.String(), actorID, reason, reference); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger_postings(credit_ledger_posting_uuid,credit_ledger_transaction_uuid,credit_ledger_account_uuid,amount_credits) VALUES(gen_random_uuid(),$1,$2,$4::bigint),(gen_random_uuid(),$1,$3,-($4::bigint))`, tID, availableID, fundingID, credits)
	return err
}
