-- +goose Up
CREATE TABLE developer_applications (
    application_uuid UUID PRIMARY KEY,
    owner_type TEXT NOT NULL CHECK (owner_type IN ('user','company')),
    user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    company_uuid UUID REFERENCES companies(company_uuid) ON DELETE RESTRICT,
    billing_account_uuid UUID NOT NULL REFERENCES billing_accounts(billing_account_uuid) ON DELETE RESTRICT,
    name VARCHAR(120) NOT NULL CHECK (length(trim(name)) > 0),
    environment TEXT NOT NULL CHECK (environment IN ('sandbox','production')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','revoked')),
    capabilities TEXT[] NOT NULL DEFAULT '{}',
    daily_credit_limit BIGINT CHECK (daily_credit_limit IS NULL OR daily_credit_limit >= 0),
    monthly_credit_limit BIGINT CHECK (monthly_credit_limit IS NULL OR monthly_credit_limit >= 0),
    max_credits_per_operation BIGINT CHECK (max_credits_per_operation IS NULL OR max_credits_per_operation >= 0),
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    CONSTRAINT chk_developer_application_owner CHECK (
        (owner_type='user' AND user_uuid IS NOT NULL AND company_uuid IS NULL) OR
        (owner_type='company' AND company_uuid IS NOT NULL AND user_uuid IS NULL)
    ),
    CONSTRAINT chk_developer_application_revoke CHECK ((status='revoked') = (revoked_at IS NOT NULL))
);

ALTER TABLE usage_operations
    ADD CONSTRAINT fk_usage_operations_application
    FOREIGN KEY (application_uuid) REFERENCES developer_applications(application_uuid) ON DELETE RESTRICT;

CREATE TABLE integration_connections (
    connection_uuid UUID PRIMARY KEY,
    application_uuid UUID NOT NULL REFERENCES developer_applications(application_uuid) ON DELETE RESTRICT,
    company_uuid UUID REFERENCES companies(company_uuid) ON DELETE RESTRICT,
    department_uuid UUID REFERENCES departments(department_uuid) ON DELETE SET NULL,
    folder_uuid UUID REFERENCES call_folders(folder_uuid) ON DELETE SET NULL,
    created_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    name VARCHAR(120) NOT NULL CHECK (length(trim(name)) > 0),
    provider TEXT NOT NULL CHECK (provider IN ('generic_api','bitrix24','amocrm','telephony')),
    status TEXT NOT NULL CHECK (status IN ('draft','active','degraded','disabled','revoked')),
    settings_version BIGINT NOT NULL DEFAULT 1 CHECK (settings_version > 0),
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_event_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_health_at TIMESTAMPTZ,
    last_error_code TEXT,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    CONSTRAINT chk_integration_connection_revoke CHECK ((status='revoked') = (revoked_at IS NOT NULL))
);

CREATE TABLE integration_service_accounts (
    service_account_uuid UUID PRIMARY KEY,
    application_uuid UUID NOT NULL REFERENCES developer_applications(application_uuid) ON DELETE RESTRICT,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE RESTRICT,
    name VARCHAR(120) NOT NULL CHECK (length(trim(name)) > 0),
    status TEXT NOT NULL CHECK (status IN ('active','disabled','revoked')),
    scopes TEXT[] NOT NULL,
    created_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    CONSTRAINT chk_service_account_revoke CHECK ((status='revoked') = (revoked_at IS NOT NULL))
);

CREATE TABLE integration_api_keys (
    key_uuid UUID PRIMARY KEY,
    service_account_uuid UUID NOT NULL REFERENCES integration_service_accounts(service_account_uuid) ON DELETE RESTRICT,
    name VARCHAR(120) NOT NULL CHECK (length(trim(name)) > 0),
    prefix TEXT NOT NULL UNIQUE,
    secret_hash BYTEA NOT NULL,
    hash_version INTEGER NOT NULL CHECK (hash_version > 0),
    scopes TEXT[] NOT NULL,
    expires_at TIMESTAMPTZ,
    overlap_until TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE integration_webhook_endpoints (
    webhook_endpoint_uuid UUID PRIMARY KEY,
    application_uuid UUID NOT NULL REFERENCES developer_applications(application_uuid) ON DELETE RESTRICT,
    connection_uuid UUID REFERENCES integration_connections(connection_uuid) ON DELETE RESTRICT,
    url_ciphertext BYTEA NOT NULL,
    key_version INTEGER NOT NULL CHECK (key_version > 0),
    signing_secret_ciphertext BYTEA NOT NULL,
    signing_key_version INTEGER NOT NULL CHECK (signing_key_version > 0),
    status TEXT NOT NULL CHECK (status IN ('active','disabled','revoked')),
    event_types TEXT[] NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE ingest_events (
    event_uuid UUID PRIMARY KEY,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE RESTRICT,
    external_event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    payload_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_sha256 BYTEA NOT NULL,
    accepted BOOLEAN NOT NULL,
    rejection_code TEXT,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (connection_uuid, external_event_id)
);

CREATE TABLE ingest_items (
    ingest_item_uuid UUID PRIMARY KEY,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE RESTRICT,
    event_uuid UUID NOT NULL REFERENCES ingest_events(event_uuid) ON DELETE RESTRICT,
    external_call_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_sha256 BYTEA NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('url','upload','connector')),
    recording_locator_ciphertext BYTEA,
    recording_locator_key_version INTEGER,
    title TEXT NOT NULL,
    original_filename TEXT,
    occurred_at TIMESTAMPTZ,
    metadata_redacted JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL CHECK (status IN ('received','processing','completed','blocked','failed','cancelled')),
    stage TEXT NOT NULL CHECK (stage IN ('received','fetching','uploading','validating','creating_call','processing','completed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 8 CHECK (max_attempts > 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    call_uuid UUID REFERENCES calls(call_uuid) ON DELETE SET NULL,
    media_sha256 BYTEA,
    media_size_bytes BIGINT CHECK (media_size_bytes IS NULL OR media_size_bytes >= 0),
    media_duration_seconds INTEGER CHECK (media_duration_seconds IS NULL OR media_duration_seconds >= 0),
    error_code TEXT,
    error_message_safe TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    UNIQUE (connection_uuid, external_call_id),
    UNIQUE (connection_uuid, idempotency_key),
    CONSTRAINT chk_ingest_locator_key CHECK ((recording_locator_ciphertext IS NULL) = (recording_locator_key_version IS NULL))
);

CREATE TABLE integration_outbox (
    outbox_uuid UUID PRIMARY KEY,
    application_uuid UUID NOT NULL REFERENCES developer_applications(application_uuid) ON DELETE RESTRICT,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE RESTRICT,
    event_id UUID NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    aggregate_uuid UUID NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','delivering','delivered','failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 12 CHECK (max_attempts > 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ
);

CREATE TABLE integration_webhook_deliveries (
    delivery_uuid UUID PRIMARY KEY,
    outbox_uuid UUID NOT NULL REFERENCES integration_outbox(outbox_uuid) ON DELETE RESTRICT,
    webhook_endpoint_uuid UUID NOT NULL REFERENCES integration_webhook_endpoints(webhook_endpoint_uuid) ON DELETE RESTRICT,
    attempt INTEGER NOT NULL CHECK (attempt > 0),
    status TEXT NOT NULL CHECK (status IN ('pending','succeeded','retryable_failed','permanent_failed')),
    http_status INTEGER,
    latency_ms BIGINT CHECK (latency_ms IS NULL OR latency_ms >= 0),
    response_size_bytes BIGINT CHECK (response_size_bytes IS NULL OR response_size_bytes >= 0),
    response_sha256 BYTEA,
    error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (outbox_uuid, webhook_endpoint_uuid, attempt)
);

CREATE TABLE integration_audit_events (
    audit_event_uuid UUID PRIMARY KEY,
    application_uuid UUID NOT NULL REFERENCES developer_applications(application_uuid) ON DELETE RESTRICT,
    connection_uuid UUID REFERENCES integration_connections(connection_uuid) ON DELETE RESTRICT,
    actor_type TEXT NOT NULL,
    actor_uuid UUID,
    event_type TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_uuid UUID NOT NULL,
    metadata_safe JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_developer_applications_owner ON developer_applications(owner_type,user_uuid,company_uuid,created_at);
CREATE INDEX idx_connections_application ON integration_connections(application_uuid,status,updated_at DESC);
CREATE INDEX idx_ingest_items_worker ON ingest_items(status,available_at,created_at,ingest_item_uuid)
    WHERE status IN ('received','processing');
CREATE INDEX idx_integration_outbox_worker ON integration_outbox(status,available_at,created_at,outbox_uuid)
    WHERE status IN ('pending','delivering');
CREATE INDEX idx_integration_audit_history ON integration_audit_events(application_uuid,created_at DESC,audit_event_uuid DESC);

-- +goose Down
DROP TABLE IF EXISTS integration_audit_events;
DROP TABLE IF EXISTS integration_webhook_deliveries;
DROP TABLE IF EXISTS integration_outbox;
DROP TABLE IF EXISTS ingest_items;
DROP TABLE IF EXISTS ingest_events;
DROP TABLE IF EXISTS integration_webhook_endpoints;
DROP TABLE IF EXISTS integration_api_keys;
DROP TABLE IF EXISTS integration_service_accounts;
DROP TABLE IF EXISTS integration_connections;
ALTER TABLE usage_operations DROP CONSTRAINT IF EXISTS fk_usage_operations_application;
DROP TABLE IF EXISTS developer_applications;
