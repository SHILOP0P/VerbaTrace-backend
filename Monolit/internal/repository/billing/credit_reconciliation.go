package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) MarkCreditOperationProviderRunning(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `UPDATE usage_operations SET status='provider_running' WHERE usage_operation_uuid=$1 AND status IN ('reserved','reconciling','provider_running')`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return models.ErrCreditOperationConflict
	}
	return nil
}
func (r *Repository) MarkCreditOperationReconciling(ctx context.Context, id uuid.UUID, reason string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var account uuid.UUID
	var app uuid.NullUUID
	err = tx.QueryRowContext(ctx, `UPDATE usage_operations SET status='reconciling',provider_usage=provider_usage||jsonb_build_object('reconciliation_reason',$2) WHERE usage_operation_uuid=$1 AND status IN ('reserved','provider_running','reconciling') RETURNING billing_account_uuid,application_uuid`, id, reason).Scan(&account, &app)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrCreditOperationConflict
	}
	if err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]any{"reason": reason})
	_, err = tx.ExecContext(ctx, `INSERT INTO billing_alerts(billing_alert_uuid,alert_type,severity,billing_account_uuid,application_uuid,usage_operation_uuid,deduplication_key,details) VALUES(gen_random_uuid(),'provider_outcome_unknown','warning',$1,$2,$3,$4,$5) ON CONFLICT(deduplication_key) DO UPDATE SET details=EXCLUDED.details,status='open',resolved_at=NULL`, account, nullableUUID(app), id, "provider-outcome:"+id.String(), details)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type ReconciliationSummary struct{ Checked, Mismatched int64 }

func (r *Repository) ReconcileCreditOperations(ctx context.Context, olderThan time.Time, limit int) (ReconciliationSummary, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ReconciliationSummary{}, err
	}
	defer func() { _ = tx.Rollback() }()
	runID, _ := uuid.NewV7()
	_, err = tx.ExecContext(ctx, `INSERT INTO billing_reconciliation_runs(billing_reconciliation_run_uuid,provider,period_start,period_end,status) VALUES($1,'all',$2,$3,'running')`, runID, olderThan.Add(-24*time.Hour), olderThan)
	if err != nil {
		return ReconciliationSummary{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT usage_operation_uuid,billing_account_uuid,application_uuid,status,maximum_charge_credits,reserved_credits FROM usage_operations WHERE completed_at IS NULL AND started_at<$1 AND status IN ('reserved','provider_running','reconciling') ORDER BY started_at LIMIT $2 FOR UPDATE SKIP LOCKED`, olderThan, limit)
	if err != nil {
		return ReconciliationSummary{}, err
	}
	defer func() { _ = rows.Close() }()
	var summary ReconciliationSummary
	var safeRelease []uuid.UUID
	type staleOperation struct {
		id              uuid.UUID
		account         uuid.UUID
		application     uuid.NullUUID
		status          string
		maximumCharge   int64
		reservedCredits int64
	}
	var staleOperations []staleOperation
	for rows.Next() {
		var operation staleOperation
		if err = rows.Scan(&operation.id, &operation.account, &operation.application, &operation.status, &operation.maximumCharge, &operation.reservedCredits); err != nil {
			return summary, err
		}
		summary.Checked++
		summary.Mismatched++
		staleOperations = append(staleOperations, operation)
	}
	if err = rows.Err(); err != nil {
		return summary, err
	}
	// lib/pq does not support issuing another statement on the same transaction
	// while a result set is still open. Materialize first, then mutate.
	if err = rows.Close(); err != nil {
		return summary, err
	}
	for _, operation := range staleOperations {
		if operation.status == "reserved" {
			safeRelease = append(safeRelease, operation.id)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE usage_operations SET status='reconciling' WHERE usage_operation_uuid=$1`, operation.id)
			if err != nil {
				return summary, err
			}
		}
		details, _ := json.Marshal(map[string]any{"status": operation.status, "maximum_charge": operation.maximumCharge, "reserved": operation.reservedCredits})
		_, err = tx.ExecContext(ctx, `INSERT INTO billing_alerts(billing_alert_uuid,alert_type,severity,billing_account_uuid,application_uuid,usage_operation_uuid,deduplication_key,details) VALUES(gen_random_uuid(),'stale_credit_reservation','critical',$1,$2,$3,$4,$5) ON CONFLICT(deduplication_key) DO UPDATE SET details=EXCLUDED.details,status='open',resolved_at=NULL`, operation.account, nullableUUID(operation.application), operation.id, "stale-reservation:"+operation.id.String(), details)
		if err != nil {
			return summary, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE billing_reconciliation_runs SET status='completed',checked_operations=$2,mismatched_operations=$3,completed_at=now() WHERE billing_reconciliation_run_uuid=$1`, runID, summary.Checked, summary.Mismatched)
	if err != nil {
		return summary, err
	}
	if err = tx.Commit(); err != nil {
		return summary, err
	}
	for _, id := range safeRelease {
		if _, settleErr := r.SettleCredits(ctx, models.SettleCreditsInput{OperationUUID: id, ActualChargeCredits: 0, ProviderCostNanoUSD: 0, ProviderUsageJSON: []byte(`{"reconciled":"released_before_provider"}`)}, time.Now().UTC()); settleErr != nil {
			return summary, settleErr
		}
		_, _ = r.db.ExecContext(ctx, `UPDATE billing_alerts SET status='resolved',resolved_at=now() WHERE deduplication_key=$1`, "stale-reservation:"+id.String())
	}
	return summary, nil
}
