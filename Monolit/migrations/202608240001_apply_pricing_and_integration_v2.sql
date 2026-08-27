-- +goose Up
ALTER TABLE plans
    ADD COLUMN monthly_price_minor BIGINT NOT NULL DEFAULT 0 CHECK (monthly_price_minor >= 0),
    ADD COLUMN currency TEXT NOT NULL DEFAULT 'RUB' CHECK (currency = 'RUB'),
    ADD COLUMN marketing_hours_hint INTEGER NOT NULL DEFAULT 0 CHECK (marketing_hours_hint >= 0),
    ADD COLUMN webhooks_enabled BOOLEAN NOT NULL DEFAULT false;

UPDATE plans SET
    monthly_price_minor = CASE code
        WHEN 'personal_start' THEN 0
        WHEN 'personal_plus' THEN 199000
        WHEN 'personal_pro' THEN 499000
        WHEN 'business_start' THEN 1490000
        WHEN 'business_plus' THEN 3990000
        WHEN 'business_pro' THEN 9990000
    END,
    monthly_credit_allowance = CASE code
        WHEN 'personal_start' THEN 250000
        WHEN 'personal_plus' THEN 1500000
        WHEN 'personal_pro' THEN 6000000
        WHEN 'business_start' THEN 7000000
        WHEN 'business_plus' THEN 30000000
        WHEN 'business_pro' THEN 90000000
    END,
    monthly_minutes_limit = CASE code
        WHEN 'personal_start' THEN 180
        WHEN 'personal_plus' THEN 1200
        WHEN 'personal_pro' THEN 4800
        WHEN 'business_start' THEN 6000
        WHEN 'business_plus' THEN 24000
        WHEN 'business_pro' THEN 72000
    END,
    marketing_hours_hint = CASE code
        WHEN 'personal_start' THEN 3
        WHEN 'personal_plus' THEN 20
        WHEN 'personal_pro' THEN 80
        WHEN 'business_start' THEN 100
        WHEN 'business_plus' THEN 400
        WHEN 'business_pro' THEN 1200
    END,
    history_retention_days = CASE code
        WHEN 'personal_start' THEN 30
        WHEN 'personal_plus' THEN 365
        WHEN 'personal_pro' THEN 365
        WHEN 'business_start' THEN 180
        WHEN 'business_plus' THEN 365
        WHEN 'business_pro' THEN 550
    END,
    export_enabled = code <> 'personal_start',
    api_access_enabled = code IN ('personal_pro','business_start','business_plus','business_pro'),
    webhooks_enabled = code IN ('personal_pro','business_start','business_plus','business_pro'),
    members_per_company_limit = CASE WHEN type='business' THEN NULL ELSE members_per_company_limit END,
    updated_at = now();

CREATE TABLE enterprise_quotes (
    enterprise_quote_uuid UUID PRIMARY KEY,
    owner_company_uuid UUID REFERENCES companies(company_uuid) ON DELETE RESTRICT,
    requested_monthly_credits BIGINT NOT NULL CHECK (requested_monthly_credits > 90000000),
    base_plan_price_minor BIGINT NOT NULL DEFAULT 9990000 CHECK (base_plan_price_minor > 0),
    base_plan_credits BIGINT NOT NULL DEFAULT 90000000 CHECK (base_plan_credits > 0),
    enterprise_markup_basis_points INTEGER NOT NULL DEFAULT 500
        CHECK (enterprise_markup_basis_points BETWEEN 0 AND 1000),
    incremental_cost_minor BIGINT NOT NULL DEFAULT 0 CHECK (incremental_cost_minor >= 0),
    custom_markup_basis_points INTEGER NOT NULL DEFAULT 1000
        CHECK (custom_markup_basis_points BETWEEN 0 AND 1000),
    monthly_price_minor BIGINT NOT NULL CHECK (monthly_price_minor > 0),
    currency TEXT NOT NULL DEFAULT 'RUB' CHECK (currency='RUB'),
    formula_version INTEGER NOT NULL DEFAULT 1 CHECK (formula_version > 0),
    inputs_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','offered','accepted','expired','revoked')),
    valid_until TIMESTAMPTZ NOT NULL,
    created_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    approved_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_enterprise_quotes_company_created
    ON enterprise_quotes(owner_company_uuid, created_at DESC);

ALTER TABLE call_folders
    ADD COLUMN system_type TEXT,
    ADD COLUMN created_by_actor_type TEXT NOT NULL DEFAULT 'user';
ALTER TABLE call_folders
    ADD CONSTRAINT chk_call_folders_system_type
        CHECK (system_type IS NULL OR system_type IN ('external_ingest')),
    ADD CONSTRAINT chk_call_folders_actor_type
        CHECK (created_by_actor_type IN ('user','system'));

CREATE UNIQUE INDEX uq_call_folders_external_personal
    ON call_folders(user_uuid)
    WHERE deleted_at IS NULL AND scope='personal' AND system_type='external_ingest';
CREATE UNIQUE INDEX uq_call_folders_external_company
    ON call_folders(company_uuid)
    WHERE deleted_at IS NULL AND scope='company' AND system_type='external_ingest';
CREATE UNIQUE INDEX uq_call_folders_external_department
    ON call_folders(company_uuid,department_uuid)
    WHERE deleted_at IS NULL AND scope='department' AND system_type='external_ingest';

ALTER TABLE integration_connections
    ADD COLUMN allow_folder_override BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE ingest_items
    ADD COLUMN destination_scope TEXT,
    ADD COLUMN destination_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    ADD COLUMN destination_company_uuid UUID REFERENCES companies(company_uuid) ON DELETE RESTRICT,
    ADD COLUMN destination_department_uuid UUID REFERENCES departments(department_uuid) ON DELETE RESTRICT,
    ADD COLUMN destination_folder_uuid UUID REFERENCES call_folders(folder_uuid) ON DELETE RESTRICT,
    ADD COLUMN placement_source TEXT NOT NULL DEFAULT 'connection_default';

UPDATE ingest_items i SET
    destination_scope = CASE
        WHEN c.department_uuid IS NOT NULL THEN 'department'
        WHEN c.company_uuid IS NOT NULL THEN 'company'
        ELSE 'personal'
    END,
    destination_user_uuid = CASE WHEN c.company_uuid IS NULL THEN c.created_by_user_uuid END,
    destination_company_uuid = c.company_uuid,
    destination_department_uuid = c.department_uuid,
    destination_folder_uuid = c.folder_uuid
FROM integration_connections c
WHERE c.connection_uuid=i.connection_uuid;

ALTER TABLE ingest_items
    ALTER COLUMN destination_scope SET NOT NULL,
    ADD CONSTRAINT chk_ingest_destination_scope CHECK (destination_scope IN ('personal','company','department')),
    ADD CONSTRAINT chk_ingest_destination_placement CHECK (
        (destination_scope='personal' AND destination_user_uuid IS NOT NULL AND destination_company_uuid IS NULL AND destination_department_uuid IS NULL)
        OR (destination_scope='company' AND destination_user_uuid IS NULL AND destination_company_uuid IS NOT NULL AND destination_department_uuid IS NULL)
        OR (destination_scope='department' AND destination_user_uuid IS NULL AND destination_company_uuid IS NOT NULL AND destination_department_uuid IS NOT NULL)
    ),
    ADD CONSTRAINT chk_ingest_placement_source CHECK (placement_source IN ('connection_default','request_override','system_external'));

-- +goose Down
ALTER TABLE ingest_items DROP CONSTRAINT IF EXISTS chk_ingest_placement_source;
ALTER TABLE ingest_items DROP CONSTRAINT IF EXISTS chk_ingest_destination_placement;
ALTER TABLE ingest_items DROP CONSTRAINT IF EXISTS chk_ingest_destination_scope;
ALTER TABLE ingest_items DROP COLUMN IF EXISTS placement_source;
ALTER TABLE ingest_items DROP COLUMN IF EXISTS destination_folder_uuid;
ALTER TABLE ingest_items DROP COLUMN IF EXISTS destination_department_uuid;
ALTER TABLE ingest_items DROP COLUMN IF EXISTS destination_company_uuid;
ALTER TABLE ingest_items DROP COLUMN IF EXISTS destination_user_uuid;
ALTER TABLE ingest_items DROP COLUMN IF EXISTS destination_scope;
ALTER TABLE integration_connections DROP COLUMN IF EXISTS allow_folder_override;
DROP INDEX IF EXISTS uq_call_folders_external_department;
DROP INDEX IF EXISTS uq_call_folders_external_company;
DROP INDEX IF EXISTS uq_call_folders_external_personal;
ALTER TABLE call_folders DROP CONSTRAINT IF EXISTS chk_call_folders_actor_type;
ALTER TABLE call_folders DROP CONSTRAINT IF EXISTS chk_call_folders_system_type;
ALTER TABLE call_folders DROP COLUMN IF EXISTS created_by_actor_type;
ALTER TABLE call_folders DROP COLUMN IF EXISTS system_type;
DROP TABLE IF EXISTS enterprise_quotes;
ALTER TABLE plans DROP COLUMN IF EXISTS webhooks_enabled;
ALTER TABLE plans DROP COLUMN IF EXISTS marketing_hours_hint;
ALTER TABLE plans DROP COLUMN IF EXISTS currency;
ALTER TABLE plans DROP COLUMN IF EXISTS monthly_price_minor;
