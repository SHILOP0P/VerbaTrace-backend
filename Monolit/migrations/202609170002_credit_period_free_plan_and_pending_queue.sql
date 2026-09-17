-- +goose Up
-- Empty means unlimited and zero means forbidden, everywhere. That rule only
-- works if "no subscription" stops being expressed as an empty limit, so it
-- becomes a plan of its own with the zeros written out.
ALTER TABLE plans DROP CONSTRAINT IF EXISTS chk_plans_code;
ALTER TABLE plans ADD CONSTRAINT chk_plans_code CHECK (code IN (
    'free',
    'personal_start', 'personal_plus', 'personal_pro',
    'business_start', 'business_plus', 'business_pro'
));

INSERT INTO plans (
    plan_uuid, code, type, name,
    monthly_minutes_limit, active_instruction_limit,
    company_limit, departments_per_company_limit, members_per_company_limit,
    instructions_per_department_limit,
    analysis_level, history_retention_days,
    export_enabled, team_analytics_enabled, api_access_enabled,
    monthly_credit_allowance, monthly_price_minor
) VALUES (
    '33333333-3333-7333-8333-333333333300', 'free', 'personal', 'Без подписки',
    0, 0,
    0, 0, 0,
    0,
    'basic', 30,
    false, false, false,
    0, 0
) ON CONFLICT (code) DO NOTHING;

-- How many calls may wait for credits before an upload is refused. Without a cap
-- a department whose limit ran out would keep piling files onto the disk for
-- nothing. NULL keeps its usual meaning of no cap.
ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS pending_credit_calls_limit INTEGER NULL
        CHECK (pending_credit_calls_limit IS NULL OR pending_credit_calls_limit >= 0);

UPDATE plans SET pending_credit_calls_limit = v.pending
FROM (VALUES
    ('free', 0),
    ('personal_start', 0),
    ('personal_plus', 5),
    ('personal_pro', 10),
    ('business_start', 20),
    ('business_plus', 40),
    ('business_pro', 80)
) AS v(code, pending)
WHERE plans.code = v.code;

-- A call that cannot be transcribed yet because the credit limit is exhausted
-- waits instead of failing. It is not an error state and must not be presented
-- as one.
ALTER TABLE calls DROP CONSTRAINT IF EXISTS chk_calls_status;
ALTER TABLE calls ADD CONSTRAINT chk_calls_status
    CHECK (status IN ('new', 'processing', 'transcribed', 'analyzed', 'failed', 'awaiting_credits'));

CREATE INDEX IF NOT EXISTS idx_calls_awaiting_credits
    ON calls (company_uuid, department_uuid)
    WHERE status = 'awaiting_credits' AND deleted_at IS NULL;

-- Embeddings call a paid provider. They were the one AI operation outside the
-- credit system entirely, so indexing and semantic search spent real money that
-- no limit could stop.
-- Priced per input token like any other model call: text-embedding-3-small is
-- $0.02 per million tokens, which is 20 nanoUSD per token.
INSERT INTO pricing_rates (
    pricing_rate_uuid, pricing_catalog_version_uuid, operation_type, provider, model, unit,
    provider_cost_nano_usd_per_unit, input_cost_nano_usd_per_token, output_cost_nano_usd_per_token
)
SELECT '33333333-3333-7333-8333-333333333351', v.pricing_catalog_version_uuid, 'embedding', 'openai', 'text-embedding-3-small', 'provider_actual_cost', 0, 20, 0
FROM pricing_catalog_versions v WHERE v.status = 'active'
ON CONFLICT (pricing_rate_uuid) DO NOTHING;

-- +goose Down
DELETE FROM pricing_rates WHERE pricing_rate_uuid = '33333333-3333-7333-8333-333333333351';

DROP INDEX IF EXISTS idx_calls_awaiting_credits;
UPDATE calls SET status = 'failed' WHERE status = 'awaiting_credits';
ALTER TABLE calls DROP CONSTRAINT IF EXISTS chk_calls_status;
ALTER TABLE calls ADD CONSTRAINT chk_calls_status
    CHECK (status IN ('new', 'processing', 'transcribed', 'analyzed', 'failed'));

ALTER TABLE plans DROP COLUMN IF EXISTS pending_credit_calls_limit;
DELETE FROM plans WHERE code = 'free';
ALTER TABLE plans DROP CONSTRAINT IF EXISTS chk_plans_code;
ALTER TABLE plans ADD CONSTRAINT chk_plans_code CHECK (code IN (
    'personal_start', 'personal_plus', 'personal_pro',
    'business_start', 'business_plus', 'business_pro'
));
