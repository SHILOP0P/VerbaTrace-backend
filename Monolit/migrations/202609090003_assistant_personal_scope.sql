-- +goose Up
ALTER TABLE plans ADD COLUMN assistant_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE plans ADD COLUMN assistant_research_enabled BOOLEAN NOT NULL DEFAULT false;
UPDATE plans SET assistant_enabled=true,assistant_research_enabled=true WHERE code NOT IN ('personal_start','business_start');
ALTER TABLE assistant_chats ALTER COLUMN company_uuid DROP NOT NULL;
ALTER TABLE assistant_runs ALTER COLUMN company_uuid DROP NOT NULL;
ALTER TABLE assistant_runs DROP CONSTRAINT assistant_runs_company_uuid_actor_user_uuid_idempotency_key_key;
ALTER TABLE assistant_runs ADD CONSTRAINT uq_assistant_scope_request UNIQUE NULLS NOT DISTINCT(company_uuid,actor_user_uuid,idempotency_key);

-- +goose Down
-- Refuse to drop personal histories implicitly during rollback.
ALTER TABLE assistant_chats ALTER COLUMN company_uuid SET NOT NULL;
ALTER TABLE assistant_runs ALTER COLUMN company_uuid SET NOT NULL;
ALTER TABLE assistant_runs DROP CONSTRAINT uq_assistant_scope_request;
ALTER TABLE assistant_runs ADD CONSTRAINT assistant_runs_company_uuid_actor_user_uuid_idempotency_key_key UNIQUE(company_uuid,actor_user_uuid,idempotency_key);
ALTER TABLE plans DROP COLUMN assistant_research_enabled;
ALTER TABLE plans DROP COLUMN assistant_enabled;
