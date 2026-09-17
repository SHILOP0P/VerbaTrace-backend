package company

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// FreezeCompany stops a company without destroying it: the owner may pay again
// or change their mind, and everything stays readable meanwhile.
func (r *Repository) FreezeCompany(ctx context.Context, companyID uuid.UUID, now time.Time) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE companies
		SET lifecycle_state='frozen', frozen_at=$2, purge_after=$3, soft_deleted_at=NULL
		WHERE company_uuid=$1 AND deleted_at IS NULL AND lifecycle_state <> 'soft_deleted'`,
		companyID, now, now.Add(models.CompanyFreezeGrace))
	if err != nil {
		return fmt.Errorf("freeze company: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return models.ErrCompanyNotFound
	}

	return nil
}

// ActivateCompany brings a frozen company back, but only while the owner's plan
// still covers one more company.
func (r *Repository) ActivateCompany(ctx context.Context, companyID uuid.UUID, now time.Time) error {
	query := `
	-- An owner without a business plan falls back to the "free" plan, where the
	-- zeros are written out. Reading a missing plan as an empty limit would mean
	-- "no cap" under the project's own rule, which is the opposite of the truth.
	WITH owner_plan AS (
	    SELECT c.manager_user_uuid,
	           COALESCE(p.company_limit, (SELECT company_limit FROM plans WHERE code = 'free')) AS company_limit
	    FROM companies c
	    LEFT JOIN subscriptions s ON s.user_uuid = c.manager_user_uuid AND s.type='business' AND s.status='active'
	    LEFT JOIN plans p ON p.plan_uuid = s.plan_uuid
	    WHERE c.company_uuid = $1
	), used AS (
	    SELECT count(*) AS active_companies
	    FROM companies c
	    JOIN owner_plan ON owner_plan.manager_user_uuid = c.manager_user_uuid
	    WHERE c.deleted_at IS NULL AND c.lifecycle_state='active' AND c.company_uuid <> $1
	)
	UPDATE companies
	SET lifecycle_state='active', frozen_at=NULL, soft_deleted_at=NULL, purge_after=NULL
	WHERE company_uuid=$1
	  AND deleted_at IS NULL
	  AND lifecycle_state='frozen'
	  AND (SELECT active_companies FROM used) < (SELECT company_limit FROM owner_plan)`

	res, err := r.db.ExecContext(ctx, query, companyID)
	if err != nil {
		return fmt.Errorf("activate company: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return models.ErrCompanyLimitExceeded
	}

	return nil
}

// GetCompanyLifecycle reports where a company stands in its lifecycle.
func (r *Repository) GetCompanyLifecycle(ctx context.Context, companyID uuid.UUID) (models.CompanyLifecycle, error) {
	var lifecycle models.CompanyLifecycle
	var frozenAt, softDeletedAt, purgeAfter sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT company_uuid, lifecycle_state, frozen_at, soft_deleted_at, purge_after, restore_used
		FROM companies WHERE company_uuid=$1`, companyID).
		Scan(&lifecycle.CompanyUUID, &lifecycle.State, &frozenAt, &softDeletedAt, &purgeAfter, &lifecycle.RestoreUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return models.CompanyLifecycle{}, models.ErrCompanyNotFound
	}
	if err != nil {
		return models.CompanyLifecycle{}, fmt.Errorf("get company lifecycle: %w", err)
	}
	lifecycle.FrozenAt = nullableTime(frozenAt)
	lifecycle.SoftDeletedAt = nullableTime(softDeletedAt)
	lifecycle.PurgeAfter = nullableTime(purgeAfter)

	return lifecycle, nil
}

// SoftDeleteExpiredFrozenCompanies moves companies that waited out their freeze
// into soft deletion, where only a superadmin can still bring them back.
func (r *Repository) SoftDeleteExpiredFrozenCompanies(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE companies
		SET lifecycle_state='soft_deleted', soft_deleted_at=$1, purge_after=$2, deleted_at=COALESCE(deleted_at,$1)
		WHERE lifecycle_state='frozen' AND purge_after IS NOT NULL AND purge_after <= $1`,
		now, now.Add(models.CompanyFreezeGrace))
	if err != nil {
		return 0, fmt.Errorf("soft delete frozen companies: %w", err)
	}
	affected, _ := res.RowsAffected()

	return affected, nil
}

// ClaimCompaniesForPurge lists companies whose soft deletion is over.
func (r *Repository) ClaimCompaniesForPurge(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT company_uuid FROM companies
		WHERE lifecycle_state='soft_deleted' AND purge_after IS NOT NULL AND purge_after <= $1
		ORDER BY purge_after LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim companies for purge: %w", err)
	}
	defer func() { _ = rows.Close() }()

	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan company for purge: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// companyPurgeSteps clears what does not disappear on its own when the company
// row goes. Two rules decide each line, and they pull in opposite directions.
//
// Personal and operational data is destroyed: ingest bookkeeping, portal user
// mappings, OAuth secrets, webhook queues. Calls are absent from the list on
// purpose — the retention worker removes them together with their files and an
// audit trail before a company is ever purged, and everything hanging off a call
// goes with it.
//
// The credit ledger and the integration audit are append-only by design, guarded
// by database triggers that reject DELETE outright. They cannot be erased, so
// the rows that carry them — billing accounts, developer applications and
// connections — survive and are merely detached from the company and revoked.
//
// Almost every foreign key here is ON DELETE RESTRICT, so the order below is the
// dependency order and not a matter of taste: children first, parents after.
var companyPurgeSteps = []string{
	// Anything that identifies people or opens a door to the customer's portal.
	`DELETE FROM integration_oauth_credentials
	 WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,
	`DELETE FROM integration_oauth_states
	 WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,
	`DELETE FROM integration_external_user_mappings
	 WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,
	`DELETE FROM integration_sync_checkpoints
	 WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,
	`DELETE FROM integration_backfill_candidates
	 WHERE backfill_uuid IN (
	     SELECT backfill_uuid FROM integration_backfills
	     WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)
	 )`,
	`DELETE FROM integration_backfills
	 WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,
	`DELETE FROM bitrix_call_candidates
	 WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,

	// Delivery queues and their endpoints.
	`DELETE FROM integration_webhook_deliveries d USING integration_webhook_endpoints e
	 WHERE d.webhook_endpoint_uuid=e.webhook_endpoint_uuid
	   AND e.connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,
	`DELETE FROM integration_webhook_endpoints
	 WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,
	`DELETE FROM integration_outbox
	 WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,
	`DELETE FROM integration_mapping_bulk_commands
	 WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,

	// Ingest bookkeeping, then the events it points at.
	`DELETE FROM ingest_items
	 WHERE destination_company_uuid=$1
	    OR connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,
	`DELETE FROM ingest_events
	 WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)`,

	// Credentials stay as rows because metered usage points at them, but they
	// stop working.
	`UPDATE integration_api_keys SET revoked_at=COALESCE(revoked_at,now())
	 WHERE service_account_uuid IN (
	     SELECT service_account_uuid FROM integration_service_accounts
	     WHERE connection_uuid IN (SELECT connection_uuid FROM integration_connections WHERE company_uuid=$1)
	 )`,
	`UPDATE integration_connections SET status='revoked', revoked_at=COALESCE(revoked_at,now()), company_uuid=NULL, department_uuid=NULL
	 WHERE company_uuid=$1`,

	// The ledger outlives the company; it just no longer names one.
	`UPDATE developer_applications SET status='revoked', revoked_at=COALESCE(revoked_at,now()), company_uuid=NULL WHERE company_uuid=$1`,
	`UPDATE billing_accounts SET company_uuid=NULL, status='closed' WHERE company_uuid=$1`,
}

// PurgeCompany removes the company for good. Members are detached first so the
// people stay, only their membership goes. It refuses while the company still
// has calls: those carry files on disk and are the retention worker's job, and
// deleting the company row around them is what used to fail on a foreign key
// every hour, forever.
func (r *Repository) PurgeCompany(ctx context.Context, companyID uuid.UUID, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("purge company: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var remainingCalls int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM calls WHERE company_uuid=$1`, companyID).Scan(&remainingCalls); err != nil {
		return fmt.Errorf("count company calls for purge: %w", err)
	}
	if remainingCalls > 0 {
		return models.ErrCompanyPurgePending
	}

	rows, err := tx.QueryContext(ctx, `SELECT user_uuid FROM company_members WHERE company_uuid=$1 AND status='active'`, companyID)
	if err != nil {
		return fmt.Errorf("list company members for purge: %w", err)
	}
	members := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan company member for purge: %w", err)
		}
		members = append(members, id)
	}
	_ = rows.Close()

	for _, member := range members {
		if err := cleanupCompanyAccess(ctx, tx, companyID, member, now); err != nil {
			return err
		}
	}

	for _, step := range companyPurgeSteps {
		if _, err := tx.ExecContext(ctx, step, companyID); err != nil {
			return fmt.Errorf("purge company: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM companies WHERE company_uuid=$1`, companyID); err != nil {
		return fmt.Errorf("purge company: %w", err)
	}

	return tx.Commit()
}

// RestoreSoftDeletedCompany gives a company one more freeze window. It is a
// superadmin decision and it can be used once per company.
func (r *Repository) RestoreSoftDeletedCompany(ctx context.Context, companyID uuid.UUID, now time.Time) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE companies
		SET lifecycle_state='frozen', frozen_at=$2, soft_deleted_at=NULL, deleted_at=NULL,
		    purge_after=$3, restore_used=true
		WHERE company_uuid=$1 AND lifecycle_state='soft_deleted' AND restore_used=false`,
		companyID, now, now.Add(models.CompanyFreezeGrace))
	if err != nil {
		return fmt.Errorf("restore company: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return models.ErrCompanyNotFound
	}

	return nil
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
