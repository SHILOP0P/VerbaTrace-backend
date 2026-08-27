package billing

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) GetCreditDashboard(ctx context.Context, subscription models.Subscription, from, to time.Time) (models.CreditDashboard, error) {
	usage, err := r.EnsureCurrentCreditUsage(ctx, subscription, time.Now().UTC())
	if err != nil {
		return models.CreditDashboard{}, err
	}
	var accountID uuid.UUID
	query := `SELECT billing_account_uuid FROM billing_accounts WHERE user_uuid=$1`
	owner := subscription.UserUUID.UUID
	if subscription.CompanyUUID.Valid {
		query, owner = `SELECT billing_account_uuid FROM billing_accounts WHERE company_uuid=$1`, subscription.CompanyUUID.UUID
	}
	if err = r.db.QueryRowContext(ctx, query, owner).Scan(&accountID); err != nil {
		return models.CreditDashboard{}, fmt.Errorf("get dashboard billing account: %w", err)
	}
	result := models.CreditDashboard{
		AllowanceCredits: usage.AllowanceCredits, AllowanceRemaining: usage.AllowanceRemaining,
		ResetsAt: usage.ResetsAt, AllowanceExhausted: usage.AllowanceRemaining == 0,
	}
	if usage.AllowanceCredits > 0 {
		result.RemainingPercent = float64(usage.AllowanceRemaining) / float64(usage.AllowanceCredits) * 100
	}
	if remaining := time.Until(usage.ResetsAt); remaining > 0 {
		result.DaysUntilReset = int((remaining + 24*time.Hour - 1) / (24 * time.Hour))
	}
	// Purchased credits are a separate balance and remain visible before the
	// monthly allowance is exhausted, so customers can verify their reserve.
	wallet := usage.WalletCredits
	result.WalletCredits = &wallet
	rows, err := r.db.QueryContext(ctx, `
		SELECT date_trunc('day',completed_at),
		       COALESCE(sum(settled_credits),0),
		       COALESCE(sum(settled_credits) FILTER (WHERE operation_type='transcription'),0),
		       COALESCE(sum(settled_credits) FILTER (WHERE operation_type='analysis'),0),
		       COALESCE(sum(settled_credits) FILTER (WHERE operation_type='deep_analysis'),0),
		       count(DISTINCT call_uuid) FILTER (WHERE call_uuid IS NOT NULL)
		FROM usage_operations
		WHERE billing_account_uuid=$1 AND environment='production' AND status='settled'
		  AND completed_at >= $2 AND completed_at < $3
		GROUP BY 1 ORDER BY 1
	`, accountID, from.UTC(), to.UTC())
	if err != nil {
		return models.CreditDashboard{}, fmt.Errorf("list credit activity: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var day models.CreditActivityDay
		if err = rows.Scan(&day.Date, &day.Credits, &day.Transcription, &day.Analysis, &day.DeepAnalysis, &day.Calls); err != nil {
			return models.CreditDashboard{}, err
		}
		result.Activity = append(result.Activity, day)
	}
	if err = rows.Err(); err != nil {
		return models.CreditDashboard{}, err
	}
	result.WalletEntries, err = r.walletEntries(ctx, accountID, 50)
	return result, err
}

func (r *Repository) walletEntries(ctx context.Context, accountID uuid.UUID, limit int) ([]models.CreditWalletEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT t.credit_ledger_transaction_uuid,t.transaction_type,
		       COALESCE(sum(p.amount_credits) FILTER (WHERE a.account_type='customer_available'),0),
		       COALESCE(t.reason,''),t.created_at
		FROM credit_ledger_transactions t
		JOIN credit_ledger_postings p ON p.credit_ledger_transaction_uuid=t.credit_ledger_transaction_uuid
		JOIN credit_ledger_accounts a ON a.credit_ledger_account_uuid=p.credit_ledger_account_uuid
		LEFT JOIN credit_grants g ON g.credit_grant_uuid=a.credit_grant_uuid
		WHERE (a.billing_account_uuid=$1 OR g.billing_account_uuid=$1)
		  AND t.transaction_type IN ('purchase','adjustment','refund','expire','reversal')
		GROUP BY t.credit_ledger_transaction_uuid,t.transaction_type,t.reason,t.created_at
		ORDER BY t.created_at DESC LIMIT $2
	`, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("list wallet entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result []models.CreditWalletEntry
	for rows.Next() {
		var item models.CreditWalletEntry
		if err = rows.Scan(&item.TransactionUUID, &item.Type, &item.Credits, &item.Reason, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err = rows.Err(); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return result, nil
}
