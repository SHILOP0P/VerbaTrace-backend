-- +goose Up
-- Repair databases that applied early versions of the billing/integration
-- migrations before their final constraints were committed.
ALTER TABLE integration_connections
    ALTER COLUMN company_uuid DROP NOT NULL;

DROP INDEX IF EXISTS uq_credit_ledger_system_account;
CREATE UNIQUE INDEX uq_credit_ledger_system_account
    ON credit_ledger_accounts(environment, account_type)
    WHERE billing_account_uuid IS NULL
      AND credit_grant_uuid IS NULL
      AND operation_uuid IS NULL;

-- +goose Down
-- This is a drift-repair migration. The canonical schema already defines
-- personal connections and the system-account index this way, so rollback is
-- intentionally a no-op.
SELECT 1;
