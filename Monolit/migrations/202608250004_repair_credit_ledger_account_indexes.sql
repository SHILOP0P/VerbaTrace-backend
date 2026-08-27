-- +goose Up
-- Restore the canonical partial unique indexes for databases that recorded an
-- early version of the billing migration before all ledger indexes existed.
DROP INDEX IF EXISTS uq_credit_ledger_grant_available;
CREATE UNIQUE INDEX uq_credit_ledger_grant_available
    ON credit_ledger_accounts(credit_grant_uuid, account_type)
    WHERE account_type = 'customer_available';

DROP INDEX IF EXISTS uq_credit_ledger_operation_reserved;
CREATE UNIQUE INDEX uq_credit_ledger_operation_reserved
    ON credit_ledger_accounts(operation_uuid, account_type)
    WHERE account_type = 'customer_reserved';

DROP INDEX IF EXISTS uq_credit_ledger_customer_consumed;
CREATE UNIQUE INDEX uq_credit_ledger_customer_consumed
    ON credit_ledger_accounts(billing_account_uuid, environment, account_type)
    WHERE account_type = 'customer_consumed';

-- +goose Down
-- Drift repair is intentionally irreversible: these indexes are part of the
-- canonical billing schema and must remain present after a rollback.
SELECT 1;
