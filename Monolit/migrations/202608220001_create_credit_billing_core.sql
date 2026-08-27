-- +goose Up
ALTER TABLE plans
    ADD COLUMN monthly_credit_allowance BIGINT;

UPDATE plans
SET monthly_credit_allowance = monthly_minutes_limit::BIGINT * 875
WHERE monthly_credit_allowance IS NULL;

ALTER TABLE plans
    ALTER COLUMN monthly_credit_allowance SET NOT NULL,
    ADD CONSTRAINT chk_plans_monthly_credit_allowance
        CHECK (monthly_credit_allowance >= 0);

CREATE TABLE pricing_catalog_versions (
    pricing_catalog_version_uuid UUID PRIMARY KEY,
    version BIGINT NOT NULL UNIQUE CHECK (version > 0),
    status TEXT NOT NULL CHECK (status IN ('draft','validated','scheduled','active','retired')),
    credit_micro_usd BIGINT NOT NULL DEFAULT 10 CHECK (credit_micro_usd > 0),
    multiplier_numerator BIGINT NOT NULL DEFAULT 7 CHECK (multiplier_numerator > 0),
    multiplier_denominator BIGINT NOT NULL DEFAULT 2 CHECK (multiplier_denominator > 0),
    effective_from TIMESTAMPTZ NOT NULL,
    effective_until TIMESTAMPTZ,
    created_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    activation_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_pricing_catalog_interval CHECK (effective_until IS NULL OR effective_until > effective_from)
);

CREATE UNIQUE INDEX uq_pricing_catalog_active
    ON pricing_catalog_versions ((status)) WHERE status = 'active';

CREATE TABLE pricing_rates (
    pricing_rate_uuid UUID PRIMARY KEY,
    pricing_catalog_version_uuid UUID NOT NULL REFERENCES pricing_catalog_versions(pricing_catalog_version_uuid) ON DELETE RESTRICT,
    operation_type TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT '',
    unit TEXT NOT NULL CHECK (unit IN ('provider_actual_cost','audio_hour','request')),
    provider_cost_nano_usd_per_unit BIGINT CHECK (provider_cost_nano_usd_per_unit IS NULL OR provider_cost_nano_usd_per_unit >= 0),
    fixed_credits_per_unit BIGINT CHECK (fixed_credits_per_unit IS NULL OR fixed_credits_per_unit >= 0),
    rounding_mode TEXT NOT NULL DEFAULT 'ceil_once' CHECK (rounding_mode = 'ceil_once'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (pricing_catalog_version_uuid, operation_type, provider, model, mode),
    CONSTRAINT chk_pricing_rate_value CHECK (
        (provider_cost_nano_usd_per_unit IS NOT NULL)::int +
        (fixed_credits_per_unit IS NOT NULL)::int = 1
    )
);

CREATE TABLE billing_accounts (
    billing_account_uuid UUID PRIMARY KEY,
    owner_type TEXT NOT NULL CHECK (owner_type IN ('user','company')),
    user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    company_uuid UUID REFERENCES companies(company_uuid) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','closed')),
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_billing_account_owner CHECK (
        (owner_type='user' AND user_uuid IS NOT NULL AND company_uuid IS NULL) OR
        (owner_type='company' AND company_uuid IS NOT NULL AND user_uuid IS NULL)
    )
);

CREATE UNIQUE INDEX uq_billing_accounts_user ON billing_accounts(user_uuid) WHERE user_uuid IS NOT NULL;
CREATE UNIQUE INDEX uq_billing_accounts_company ON billing_accounts(company_uuid) WHERE company_uuid IS NOT NULL;

CREATE TABLE allowance_epochs (
    allowance_epoch_uuid UUID PRIMARY KEY,
    billing_account_uuid UUID NOT NULL REFERENCES billing_accounts(billing_account_uuid) ON DELETE RESTRICT,
    subscription_uuid UUID NOT NULL REFERENCES subscriptions(subscription_uuid) ON DELETE RESTRICT,
    policy TEXT NOT NULL DEFAULT 'calendar_month_utc' CHECK (policy='calendar_month_utc'),
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    allowance_credits BIGINT NOT NULL CHECK (allowance_credits >= 0),
    status TEXT NOT NULL CHECK (status IN ('open','closed','reset')),
    reset_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    reset_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at TIMESTAMPTZ,
    CONSTRAINT chk_allowance_epoch_interval CHECK (ends_at > starts_at),
    CONSTRAINT chk_allowance_epoch_reset CHECK (
        (status <> 'reset' AND reset_by_user_uuid IS NULL AND reset_reason IS NULL) OR
        (status = 'reset' AND reset_by_user_uuid IS NOT NULL AND length(trim(reset_reason)) > 0)
    ),
    UNIQUE (subscription_uuid, starts_at)
);

CREATE UNIQUE INDEX uq_allowance_epoch_open
    ON allowance_epochs(subscription_uuid) WHERE status='open';

CREATE TABLE credit_grants (
    credit_grant_uuid UUID PRIMARY KEY,
    billing_account_uuid UUID NOT NULL REFERENCES billing_accounts(billing_account_uuid) ON DELETE RESTRICT,
    allowance_epoch_uuid UUID REFERENCES allowance_epochs(allowance_epoch_uuid) ON DELETE RESTRICT,
    grant_type TEXT NOT NULL CHECK (grant_type IN ('subscription_allowance','promotional','purchased','sandbox')),
    environment TEXT NOT NULL CHECK (environment IN ('production','sandbox')),
    original_credits BIGINT NOT NULL CHECK (original_credits >= 0),
    expires_at TIMESTAMPTZ,
    source_reference TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_credit_grant_environment CHECK (
        (grant_type='sandbox' AND environment='sandbox') OR
        (grant_type<>'sandbox' AND environment='production')
    ),
    CONSTRAINT chk_allowance_grant_epoch CHECK (
        (grant_type='subscription_allowance' AND allowance_epoch_uuid IS NOT NULL) OR
        (grant_type<>'subscription_allowance' AND allowance_epoch_uuid IS NULL)
    ),
    UNIQUE (billing_account_uuid, source_reference)
);

CREATE TABLE credit_ledger_accounts (
    credit_ledger_account_uuid UUID PRIMARY KEY,
    billing_account_uuid UUID REFERENCES billing_accounts(billing_account_uuid) ON DELETE RESTRICT,
    credit_grant_uuid UUID REFERENCES credit_grants(credit_grant_uuid) ON DELETE RESTRICT,
    environment TEXT NOT NULL CHECK (environment IN ('production','sandbox')),
    account_type TEXT NOT NULL CHECK (account_type IN (
        'customer_available','customer_reserved','customer_consumed',
        'funding_source','expiry_source','internal_pricing_loss'
    )),
    operation_uuid UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_credit_ledger_grant_available
    ON credit_ledger_accounts(credit_grant_uuid, account_type)
    WHERE account_type='customer_available';
CREATE UNIQUE INDEX uq_credit_ledger_operation_reserved
    ON credit_ledger_accounts(operation_uuid, account_type)
    WHERE account_type='customer_reserved';
CREATE UNIQUE INDEX uq_credit_ledger_system_account
    ON credit_ledger_accounts(environment, account_type)
    WHERE billing_account_uuid IS NULL AND credit_grant_uuid IS NULL AND operation_uuid IS NULL;
CREATE UNIQUE INDEX uq_credit_ledger_customer_consumed
    ON credit_ledger_accounts(billing_account_uuid, environment, account_type)
    WHERE account_type='customer_consumed';

CREATE TABLE credit_ledger_transactions (
    credit_ledger_transaction_uuid UUID PRIMARY KEY,
    transaction_type TEXT NOT NULL CHECK (transaction_type IN (
        'grant','purchase','reserve','settle','release','refund','expire',
        'adjustment','allowance_opened','allowance_reset','allowance_closed',
        'reversal','internal_pricing_loss'
    )),
    idempotency_key TEXT NOT NULL UNIQUE,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('system','user','admin','provider')),
    actor_uuid UUID,
    reason TEXT,
    source_reference TEXT,
    reversal_of_transaction_uuid UUID REFERENCES credit_ledger_transactions(credit_ledger_transaction_uuid) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_credit_reversal CHECK (
        (transaction_type='reversal' AND reversal_of_transaction_uuid IS NOT NULL) OR
        (transaction_type<>'reversal' AND reversal_of_transaction_uuid IS NULL)
    )
);

CREATE SEQUENCE credit_ledger_posting_sequence;

CREATE TABLE credit_ledger_postings (
    credit_ledger_posting_uuid UUID PRIMARY KEY,
    credit_ledger_transaction_uuid UUID NOT NULL REFERENCES credit_ledger_transactions(credit_ledger_transaction_uuid) ON DELETE RESTRICT,
    credit_ledger_account_uuid UUID NOT NULL REFERENCES credit_ledger_accounts(credit_ledger_account_uuid) ON DELETE RESTRICT,
    amount_credits BIGINT NOT NULL CHECK (amount_credits <> 0),
    account_sequence BIGINT NOT NULL DEFAULT nextval('credit_ledger_posting_sequence') CHECK (account_sequence > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (credit_ledger_account_uuid, account_sequence)
);

-- +goose StatementBegin
CREATE FUNCTION enforce_credit_transaction_balanced() RETURNS trigger AS $$
DECLARE
    v_transaction UUID := COALESCE(NEW.credit_ledger_transaction_uuid, OLD.credit_ledger_transaction_uuid);
    v_count BIGINT;
    v_sum NUMERIC;
BEGIN
    SELECT count(*), COALESCE(sum(amount_credits::numeric), 0)
      INTO v_count, v_sum
      FROM credit_ledger_postings
     WHERE credit_ledger_transaction_uuid = v_transaction;
    IF v_count < 2 OR v_sum <> 0 THEN
        RAISE EXCEPTION 'credit ledger transaction % is not balanced: postings %, sum %', v_transaction, v_count, v_sum;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER trg_credit_transaction_balanced
AFTER INSERT OR UPDATE OR DELETE ON credit_ledger_postings
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION enforce_credit_transaction_balanced();

-- +goose StatementBegin
CREATE FUNCTION reject_credit_ledger_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'credit ledger is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_credit_transactions_immutable
BEFORE UPDATE OR DELETE ON credit_ledger_transactions
FOR EACH ROW EXECUTE FUNCTION reject_credit_ledger_mutation();

CREATE TRIGGER trg_credit_postings_immutable
BEFORE UPDATE OR DELETE ON credit_ledger_postings
FOR EACH ROW EXECUTE FUNCTION reject_credit_ledger_mutation();

CREATE TABLE usage_operations (
    usage_operation_uuid UUID PRIMARY KEY,
    billing_account_uuid UUID NOT NULL REFERENCES billing_accounts(billing_account_uuid) ON DELETE RESTRICT,
    application_uuid UUID,
    call_uuid UUID REFERENCES calls(call_uuid) ON DELETE SET NULL,
    root_operation_uuid UUID REFERENCES usage_operations(usage_operation_uuid) ON DELETE RESTRICT,
    operation_type TEXT NOT NULL,
    environment TEXT NOT NULL CHECK (environment IN ('production','sandbox')),
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT '',
    provider_request_id TEXT,
    pricing_catalog_version_uuid UUID NOT NULL REFERENCES pricing_catalog_versions(pricing_catalog_version_uuid) ON DELETE RESTRICT,
    idempotency_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'estimating','reserved','provider_running','settled','released',
        'blocked_insufficient_credits','reconciling','failed'
    )),
    maximum_charge_credits BIGINT NOT NULL CHECK (maximum_charge_credits >= 0),
    reserved_credits BIGINT NOT NULL DEFAULT 0 CHECK (reserved_credits >= 0),
    settled_credits BIGINT NOT NULL DEFAULT 0 CHECK (settled_credits >= 0),
    provider_cost_nano_usd BIGINT CHECK (provider_cost_nano_usd IS NULL OR provider_cost_nano_usd >= 0),
    internal_pricing_loss_credits BIGINT NOT NULL DEFAULT 0 CHECK (internal_pricing_loss_credits >= 0),
    provider_usage JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    UNIQUE (billing_account_uuid, environment, idempotency_key),
    CONSTRAINT chk_usage_operation_cap CHECK (settled_credits <= maximum_charge_credits),
    CONSTRAINT chk_usage_operation_reserve CHECK (reserved_credits <= maximum_charge_credits)
);

CREATE TABLE usage_operation_reservations (
    usage_operation_reservation_uuid UUID PRIMARY KEY,
    usage_operation_uuid UUID NOT NULL REFERENCES usage_operations(usage_operation_uuid) ON DELETE RESTRICT,
    credit_grant_uuid UUID NOT NULL REFERENCES credit_grants(credit_grant_uuid) ON DELETE RESTRICT,
    reserved_credits BIGINT NOT NULL CHECK (reserved_credits > 0),
    settled_credits BIGINT NOT NULL DEFAULT 0 CHECK (settled_credits >= 0),
    released_credits BIGINT NOT NULL DEFAULT 0 CHECK (released_credits >= 0),
    spend_order INTEGER NOT NULL CHECK (spend_order > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (usage_operation_uuid, credit_grant_uuid),
    UNIQUE (usage_operation_uuid, spend_order),
    CONSTRAINT chk_usage_reservation_allocation CHECK (settled_credits + released_credits <= reserved_credits)
);

CREATE TABLE allowance_reset_batches (
    allowance_reset_batch_uuid UUID PRIMARY KEY,
    filter_snapshot JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft','approved','running','paused','completed','failed')),
    reason TEXT NOT NULL CHECK (length(trim(reason)) > 0),
    idempotency_key TEXT NOT NULL UNIQUE,
    requested_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    approved_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    total_items INTEGER NOT NULL DEFAULT 0 CHECK (total_items >= 0),
    succeeded_items INTEGER NOT NULL DEFAULT 0 CHECK (succeeded_items >= 0),
    failed_items INTEGER NOT NULL DEFAULT 0 CHECK (failed_items >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT chk_reset_batch_approval CHECK (approved_by_user_uuid IS NULL OR approved_by_user_uuid <> requested_by_user_uuid)
);

CREATE TABLE allowance_reset_items (
    allowance_reset_item_uuid UUID PRIMARY KEY,
    allowance_reset_batch_uuid UUID NOT NULL REFERENCES allowance_reset_batches(allowance_reset_batch_uuid) ON DELETE RESTRICT,
    subscription_uuid UUID NOT NULL REFERENCES subscriptions(subscription_uuid) ON DELETE RESTRICT,
    previous_epoch_uuid UUID REFERENCES allowance_epochs(allowance_epoch_uuid) ON DELETE RESTRICT,
    new_epoch_uuid UUID REFERENCES allowance_epochs(allowance_epoch_uuid) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('pending','running','succeeded','failed')),
    error_code TEXT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (allowance_reset_batch_uuid, subscription_uuid)
);

CREATE TABLE billing_reconciliation_runs (
    billing_reconciliation_run_uuid UUID PRIMARY KEY,
    provider TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running','completed','failed')),
    checked_operations BIGINT NOT NULL DEFAULT 0,
    mismatched_operations BIGINT NOT NULL DEFAULT 0,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CHECK (period_end > period_start)
);

CREATE INDEX idx_credit_grants_spend_order
    ON credit_grants(billing_account_uuid, environment, grant_type, expires_at, created_at);
CREATE INDEX idx_credit_postings_transaction
    ON credit_ledger_postings(credit_ledger_transaction_uuid);
CREATE INDEX idx_usage_operations_status
    ON usage_operations(status, started_at, usage_operation_uuid);
CREATE INDEX idx_usage_reservations_operation
    ON usage_operation_reservations(usage_operation_uuid, spend_order);
CREATE INDEX idx_allowance_epochs_account_period
    ON allowance_epochs(billing_account_uuid, starts_at DESC);

INSERT INTO pricing_catalog_versions (
    pricing_catalog_version_uuid, version, status, credit_micro_usd,
    multiplier_numerator, multiplier_denominator, effective_from,
    activation_reason
) VALUES (
    '33333333-3333-7333-8333-333333333331', 1, 'active', 10, 7, 2,
    '2026-08-22T00:00:00Z', 'initial credit pricing policy'
);

INSERT INTO pricing_rates (
    pricing_rate_uuid, pricing_catalog_version_uuid, operation_type, provider,
    model, mode, unit, provider_cost_nano_usd_per_unit
) VALUES
    ('33333333-3333-7333-8333-333333333341','33333333-3333-7333-8333-333333333331','transcription','assemblyai','universal-2','standard','audio_hour',150000000),
    ('33333333-3333-7333-8333-333333333342','33333333-3333-7333-8333-333333333331','transcription','assemblyai','universal-2','diarized','audio_hour',170000000),
    ('33333333-3333-7333-8333-333333333343','33333333-3333-7333-8333-333333333331','transcription','assemblyai','universal-2','identified','audio_hour',190000000),
    ('33333333-3333-7333-8333-333333333344','33333333-3333-7333-8333-333333333331','analysis','openrouter','openai/gpt-5-mini','','provider_actual_cost',0),
    ('33333333-3333-7333-8333-333333333345','33333333-3333-7333-8333-333333333331','deep_analysis','openrouter','openai/gpt-5-mini','','provider_actual_cost',0);

-- The application FK is installed by the developer-platform migration after
-- developer_applications exists. Keeping this migration deployable independently
-- is required for the shadow-metering rollout.

-- +goose Down
DROP TABLE IF EXISTS billing_reconciliation_runs;
DROP TABLE IF EXISTS allowance_reset_items;
DROP TABLE IF EXISTS allowance_reset_batches;
DROP TABLE IF EXISTS usage_operation_reservations;
DROP TABLE IF EXISTS usage_operations;
DROP TRIGGER IF EXISTS trg_credit_postings_immutable ON credit_ledger_postings;
DROP TRIGGER IF EXISTS trg_credit_transactions_immutable ON credit_ledger_transactions;
DROP FUNCTION IF EXISTS reject_credit_ledger_mutation();
DROP TRIGGER IF EXISTS trg_credit_transaction_balanced ON credit_ledger_postings;
DROP FUNCTION IF EXISTS enforce_credit_transaction_balanced();
DROP TABLE IF EXISTS credit_ledger_postings;
DROP SEQUENCE IF EXISTS credit_ledger_posting_sequence;
DROP TABLE IF EXISTS credit_ledger_transactions;
DROP TABLE IF EXISTS credit_ledger_accounts;
DROP TABLE IF EXISTS credit_grants;
DROP TABLE IF EXISTS allowance_epochs;
DROP TABLE IF EXISTS billing_accounts;
DROP TABLE IF EXISTS pricing_rates;
DROP INDEX IF EXISTS uq_pricing_catalog_active;
DROP TABLE IF EXISTS pricing_catalog_versions;
ALTER TABLE plans DROP CONSTRAINT IF EXISTS chk_plans_monthly_credit_allowance;
ALTER TABLE plans DROP COLUMN IF EXISTS monthly_credit_allowance;
