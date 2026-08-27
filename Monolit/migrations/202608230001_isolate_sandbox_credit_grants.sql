-- +goose Up
ALTER TABLE credit_grants
    ADD COLUMN application_uuid UUID REFERENCES developer_applications(application_uuid) ON DELETE RESTRICT;

UPDATE credit_grants g
SET application_uuid = substring(g.source_reference FROM '^sandbox-application:([0-9a-fA-F-]{36})$')::uuid
WHERE g.grant_type='sandbox'
  AND g.source_reference ~ '^sandbox-application:[0-9a-fA-F-]{36}$';

ALTER TABLE credit_grants
    ADD CONSTRAINT chk_credit_grant_application_scope CHECK (
        (grant_type='sandbox' AND application_uuid IS NOT NULL) OR
        (grant_type<>'sandbox' AND application_uuid IS NULL)
    );

CREATE INDEX idx_credit_grants_sandbox_application
    ON credit_grants(application_uuid,created_at)
    WHERE grant_type='sandbox';

-- +goose Down
DROP INDEX IF EXISTS idx_credit_grants_sandbox_application;
ALTER TABLE credit_grants DROP CONSTRAINT IF EXISTS chk_credit_grant_application_scope;
ALTER TABLE credit_grants DROP COLUMN IF EXISTS application_uuid;
