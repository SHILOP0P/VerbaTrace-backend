-- +goose Up
ALTER TABLE integration_connections
    ADD COLUMN disable_policy TEXT NOT NULL DEFAULT 'pause'
        CHECK (disable_policy IN ('continue','pause','cancel')),
    ADD COLUMN disabled_at TIMESTAMPTZ;

ALTER TABLE integration_webhook_endpoints
    ADD COLUMN name VARCHAR(120) NOT NULL DEFAULT 'Webhook'
        CHECK (length(trim(name)) > 0),
    ADD COLUMN lock_version BIGINT NOT NULL DEFAULT 1 CHECK (lock_version > 0),
    ADD CONSTRAINT chk_integration_webhook_revoke
        CHECK ((status='revoked') = (revoked_at IS NOT NULL));

ALTER TABLE ingest_items
    ADD COLUMN application_uuid UUID REFERENCES developer_applications(application_uuid) ON DELETE RESTRICT,
    ADD COLUMN billing_account_uuid UUID REFERENCES billing_accounts(billing_account_uuid) ON DELETE RESTRICT,
    ADD COLUMN connection_settings_version BIGINT NOT NULL DEFAULT 1 CHECK (connection_settings_version > 0),
    ADD COLUMN placement_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN instruction_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN locator_expires_at TIMESTAMPTZ,
    ADD COLUMN lease_expires_at TIMESTAMPTZ;

UPDATE ingest_items i
   SET application_uuid=c.application_uuid,
       billing_account_uuid=a.billing_account_uuid,
       connection_settings_version=c.settings_version,
       placement_snapshot=jsonb_build_object('company_uuid',c.company_uuid,'department_uuid',c.department_uuid,'folder_uuid',c.folder_uuid)
  FROM integration_connections c
  JOIN developer_applications a ON a.application_uuid=c.application_uuid
 WHERE i.connection_uuid=c.connection_uuid;

ALTER TABLE ingest_items
    ALTER COLUMN application_uuid SET NOT NULL,
    ALTER COLUMN billing_account_uuid SET NOT NULL;

ALTER TABLE calls
    ADD COLUMN integration_connection_uuid UUID REFERENCES integration_connections(connection_uuid) ON DELETE SET NULL,
    ADD COLUMN ingest_item_uuid UUID REFERENCES ingest_items(ingest_item_uuid) ON DELETE SET NULL;
CREATE UNIQUE INDEX uq_calls_ingest_item ON calls(ingest_item_uuid) WHERE ingest_item_uuid IS NOT NULL;

CREATE TABLE billing_alerts (
    billing_alert_uuid UUID PRIMARY KEY,
    alert_type TEXT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('warning','critical')),
    billing_account_uuid UUID REFERENCES billing_accounts(billing_account_uuid) ON DELETE RESTRICT,
    application_uuid UUID REFERENCES developer_applications(application_uuid) ON DELETE RESTRICT,
    usage_operation_uuid UUID REFERENCES usage_operations(usage_operation_uuid) ON DELETE RESTRICT,
    deduplication_key TEXT NOT NULL UNIQUE,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','acknowledged','resolved')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

CREATE INDEX idx_ingest_items_lease ON ingest_items(status,lease_expires_at,available_at,created_at)
    WHERE status IN ('received','processing','blocked');
CREATE INDEX idx_outbox_lease ON integration_outbox(status,locked_at,available_at,created_at)
    WHERE status IN ('pending','delivering');
CREATE INDEX idx_webhook_endpoints_connection ON integration_webhook_endpoints(application_uuid,connection_uuid,status);
CREATE INDEX idx_usage_application_window ON usage_operations(application_uuid,started_at,status)
    WHERE application_uuid IS NOT NULL;

-- Enforce application/connection identity at the database boundary.
ALTER TABLE integration_connections ADD CONSTRAINT uq_connection_application UNIQUE(connection_uuid,application_uuid);
ALTER TABLE integration_service_accounts ADD CONSTRAINT fk_service_account_connection_application
    FOREIGN KEY(connection_uuid,application_uuid)
    REFERENCES integration_connections(connection_uuid,application_uuid) ON DELETE RESTRICT;

-- Audit is append-only for the application role, just like the financial ledger.
CREATE TRIGGER trg_integration_audit_immutable
BEFORE UPDATE OR DELETE ON integration_audit_events
FOR EACH ROW EXECUTE FUNCTION reject_credit_ledger_mutation();

-- +goose Down
DROP TRIGGER IF EXISTS trg_integration_audit_immutable ON integration_audit_events;
ALTER TABLE integration_service_accounts DROP CONSTRAINT IF EXISTS fk_service_account_connection_application;
ALTER TABLE integration_connections DROP CONSTRAINT IF EXISTS uq_connection_application;
DROP INDEX IF EXISTS idx_usage_application_window;
DROP INDEX IF EXISTS idx_webhook_endpoints_connection;
DROP INDEX IF EXISTS idx_outbox_lease;
DROP INDEX IF EXISTS idx_ingest_items_lease;
DROP TABLE IF EXISTS billing_alerts;
DROP INDEX IF EXISTS uq_calls_ingest_item;
ALTER TABLE calls DROP COLUMN IF EXISTS ingest_item_uuid;
ALTER TABLE calls DROP COLUMN IF EXISTS integration_connection_uuid;
ALTER TABLE ingest_items
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS locator_expires_at,
    DROP COLUMN IF EXISTS instruction_snapshot,
    DROP COLUMN IF EXISTS placement_snapshot,
    DROP COLUMN IF EXISTS connection_settings_version,
    DROP COLUMN IF EXISTS billing_account_uuid,
    DROP COLUMN IF EXISTS application_uuid;
ALTER TABLE integration_webhook_endpoints
    DROP CONSTRAINT IF EXISTS chk_integration_webhook_revoke,
    DROP COLUMN IF EXISTS lock_version,
    DROP COLUMN IF EXISTS name;
ALTER TABLE integration_connections
    DROP COLUMN IF EXISTS disabled_at,
    DROP COLUMN IF EXISTS disable_policy;
