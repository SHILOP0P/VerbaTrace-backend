-- +goose Up
UPDATE plans SET history_retention_days = CASE code
    WHEN 'business_start' THEN 180
    WHEN 'business_plus' THEN 365
    WHEN 'business_pro' THEN 550
    ELSE history_retention_days
END, updated_at = now()
WHERE code IN ('business_start','business_plus','business_pro');

-- +goose StatementBegin
DO $$
BEGIN
    IF (SELECT count(*) FROM plans WHERE code IN ('business_start','business_plus','business_pro')) <> 3 THEN
        RAISE EXCEPTION 'expected business_start, business_plus and business_pro plans';
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE calls
    ADD COLUMN retention_base_at TIMESTAMPTZ,
    ADD COLUMN retention_days_at_creation INTEGER,
    ADD COLUMN retention_expires_at TIMESTAMPTZ,
    ADD COLUMN retention_state TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN retention_hold_until TIMESTAMPTZ,
    ADD COLUMN retention_hold_reason TEXT,
    ADD COLUMN retention_version BIGINT NOT NULL DEFAULT 1;

WITH effective_retention AS (
    SELECT c.call_uuid,
           COALESCE(cp.history_retention_days, up.history_retention_days, 30) AS days
    FROM calls c
    LEFT JOIN LATERAL (
        SELECT p.history_retention_days
        FROM subscriptions s JOIN plans p ON p.plan_uuid=s.plan_uuid
        WHERE s.company_uuid=c.company_uuid AND s.status='active'
        ORDER BY s.updated_at DESC LIMIT 1
    ) cp ON true
    LEFT JOIN LATERAL (
        SELECT p.history_retention_days
        FROM subscriptions s JOIN plans p ON p.plan_uuid=s.plan_uuid
        WHERE s.user_uuid=c.uploaded_by_user_uuid AND s.status='active'
        ORDER BY s.updated_at DESC LIMIT 1
    ) up ON true
)
UPDATE calls c SET
    retention_base_at=c.created_at,
    retention_days_at_creation=e.days,
    retention_expires_at=CASE WHEN c.created_at + make_interval(days => e.days) <= now()
        THEN now() + interval '30 days' ELSE c.created_at + make_interval(days => e.days) END,
    retention_state=CASE WHEN c.created_at + make_interval(days => e.days) <= now()
        THEN 'grace' ELSE 'active' END
FROM effective_retention e WHERE e.call_uuid=c.call_uuid;

ALTER TABLE calls
    ALTER COLUMN retention_base_at SET NOT NULL,
    ALTER COLUMN retention_days_at_creation SET NOT NULL,
    ALTER COLUMN retention_expires_at SET NOT NULL,
    ADD CONSTRAINT chk_calls_retention_days CHECK (retention_days_at_creation > 0),
    ADD CONSTRAINT chk_calls_retention_state CHECK (retention_state IN ('active','grace','queued','deleting','deletion_failed','blocked')),
    ADD CONSTRAINT chk_calls_retention_version CHECK (retention_version > 0),
    ADD CONSTRAINT chk_calls_retention_hold CHECK ((retention_hold_until IS NULL) = (retention_hold_reason IS NULL));

CREATE INDEX idx_calls_retention_due ON calls(retention_state,retention_expires_at,call_uuid)
    WHERE retention_state IN ('active','grace','deletion_failed');

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION set_call_retention() RETURNS trigger AS $$
DECLARE v_days integer;
BEGIN
    IF NEW.retention_expires_at IS NOT NULL THEN RETURN NEW; END IF;
    IF NEW.company_uuid IS NOT NULL THEN
        SELECT p.history_retention_days INTO v_days
        FROM subscriptions s JOIN plans p ON p.plan_uuid=s.plan_uuid
        WHERE s.company_uuid=NEW.company_uuid AND s.status='active'
        ORDER BY s.updated_at DESC LIMIT 1;
    END IF;
    IF v_days IS NULL THEN
        SELECT p.history_retention_days INTO v_days
        FROM subscriptions s JOIN plans p ON p.plan_uuid=s.plan_uuid
        WHERE s.user_uuid=NEW.uploaded_by_user_uuid AND s.status='active'
        ORDER BY s.updated_at DESC LIMIT 1;
    END IF;
    v_days := COALESCE(v_days, 30);
    NEW.retention_base_at := NEW.created_at;
    NEW.retention_days_at_creation := v_days;
    NEW.retention_expires_at := NEW.created_at + make_interval(days => v_days);
    RETURN NEW;
END $$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER calls_set_retention BEFORE INSERT ON calls
FOR EACH ROW EXECUTE FUNCTION set_call_retention();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION extend_calls_after_plan_upgrade() RETURNS trigger AS $$
DECLARE v_days integer;
BEGIN
    IF NEW.status <> 'active' THEN RETURN NEW; END IF;
    SELECT history_retention_days INTO v_days FROM plans WHERE plan_uuid=NEW.plan_uuid;
    UPDATE calls SET
        retention_days_at_creation=GREATEST(retention_days_at_creation,v_days),
        retention_expires_at=GREATEST(retention_expires_at,retention_base_at+make_interval(days=>v_days)),
        retention_state=CASE WHEN retention_state='grace' AND retention_base_at+make_interval(days=>v_days)>now() THEN 'active' ELSE retention_state END,
        retention_version=retention_version+1
    WHERE ((NEW.company_uuid IS NOT NULL AND company_uuid=NEW.company_uuid)
        OR (NEW.company_uuid IS NULL AND company_uuid IS NULL AND uploaded_by_user_uuid=NEW.user_uuid))
      AND retention_base_at+make_interval(days=>v_days)>retention_expires_at
      AND retention_state NOT IN ('deleting','blocked');
    RETURN NEW;
END $$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER subscriptions_extend_call_retention
AFTER INSERT OR UPDATE OF plan_uuid,status ON subscriptions
FOR EACH ROW EXECUTE FUNCTION extend_calls_after_plan_upgrade();

CREATE TABLE retention_call_deletions (
    deletion_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL UNIQUE,
    retention_expires_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('claimed','db_deleted','files_pending','completed','failed','blocked')),
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    last_error_code TEXT,
    last_error_message_safe TEXT,
    db_manifest JSONB NOT NULL DEFAULT '{}'::jsonb,
    file_manifest JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX idx_retention_deletions_work ON retention_call_deletions(status,available_at,created_at)
    WHERE status IN ('claimed','db_deleted','files_pending','failed');

CREATE TABLE retention_audit_events (
    event_uuid UUID PRIMARY KEY,
    run_uuid UUID NOT NULL,
    deletion_uuid UUID,
    entity_type TEXT NOT NULL,
    entity_uuid UUID,
    event_type TEXT NOT NULL,
    item_count BIGINT,
    byte_count BIGINT,
    metadata_safe JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_retention_audit_run ON retention_audit_events(run_uuid,created_at,event_uuid);
CREATE INDEX idx_retention_audit_entity ON retention_audit_events(entity_type,entity_uuid,created_at DESC);

ALTER TABLE analysis_instructions
    ADD COLUMN deleted_at TIMESTAMPTZ,
    ADD COLUMN deleted_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    ADD COLUMN deletion_reason TEXT,
    ADD COLUMN purge_state TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN purge_after TIMESTAMPTZ,
    ADD CONSTRAINT chk_instruction_purge_state CHECK(purge_state IN ('active','eligible','queued','failed'));

CREATE TABLE analysis_instruction_versions (
    instruction_version_uuid UUID PRIMARY KEY,
    instruction_uuid UUID NOT NULL REFERENCES analysis_instructions(instruction_uuid) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK(version>0),
    title_snapshot TEXT NOT NULL,
    scope_snapshot TEXT NOT NULL,
    original_filename TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    content_sha256 TEXT NOT NULL,
    file_path TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'published' CHECK(status IN ('draft','published','retired')),
    created_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    UNIQUE(instruction_uuid,version)
);
CREATE INDEX idx_instruction_versions_instruction ON analysis_instruction_versions(instruction_uuid,version DESC);

INSERT INTO analysis_instruction_versions(
    instruction_version_uuid,instruction_uuid,version,title_snapshot,scope_snapshot,
    original_filename,mime_type,size_bytes,content_sha256,file_path,status,
    created_by_user_uuid,created_at,published_at
)
SELECT gen_random_uuid(),instruction_uuid,1,title,scope,original_filename,mime_type,
       size_bytes,content_sha256,file_path,'published',created_by_user_uuid,created_at,created_at
FROM analysis_instructions;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION version_analysis_instruction() RETURNS trigger AS $$
DECLARE next_version integer;
BEGIN
    IF TG_OP='UPDATE' AND NEW.title IS NOT DISTINCT FROM OLD.title
       AND NEW.file_path IS NOT DISTINCT FROM OLD.file_path
       AND NEW.content_sha256 IS NOT DISTINCT FROM OLD.content_sha256
       AND NEW.original_filename IS NOT DISTINCT FROM OLD.original_filename THEN
        RETURN NEW;
    END IF;
    SELECT COALESCE(max(version),0)+1 INTO next_version
    FROM analysis_instruction_versions WHERE instruction_uuid=NEW.instruction_uuid;
    INSERT INTO analysis_instruction_versions(
        instruction_version_uuid,instruction_uuid,version,title_snapshot,scope_snapshot,
        original_filename,mime_type,size_bytes,content_sha256,file_path,status,
        created_by_user_uuid,created_at,published_at
    ) VALUES(gen_random_uuid(),NEW.instruction_uuid,next_version,NEW.title,NEW.scope,
        NEW.original_filename,NEW.mime_type,NEW.size_bytes,NEW.content_sha256,NEW.file_path,
        'published',NEW.created_by_user_uuid,now(),now());
    RETURN NEW;
END $$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER analysis_instructions_create_version
AFTER INSERT OR UPDATE OF title,file_path,content_sha256,original_filename ON analysis_instructions
FOR EACH ROW EXECUTE FUNCTION version_analysis_instruction();

CREATE TABLE call_analysis_instruction_snapshots (
    analysis_uuid UUID NOT NULL REFERENCES call_analyses(analysis_uuid) ON DELETE CASCADE,
    instruction_version_uuid UUID NOT NULL REFERENCES analysis_instruction_versions(instruction_version_uuid) ON DELETE RESTRICT,
    instruction_uuid UUID NOT NULL,
    position INTEGER NOT NULL CHECK(position>=0),
    selection_source TEXT NOT NULL CHECK(selection_source IN ('explicit','personal','company','department','folder')),
    title_snapshot TEXT NOT NULL,
    scope_snapshot TEXT NOT NULL,
    content_sha256 TEXT NOT NULL,
    content_snapshot TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(analysis_uuid,instruction_version_uuid)
);
CREATE INDEX idx_analysis_instruction_snapshots_instruction ON call_analysis_instruction_snapshots(instruction_uuid,analysis_uuid);

-- +goose Down
DROP TABLE IF EXISTS call_analysis_instruction_snapshots;
DROP TRIGGER IF EXISTS analysis_instructions_create_version ON analysis_instructions;
DROP FUNCTION IF EXISTS version_analysis_instruction();
DROP TABLE IF EXISTS analysis_instruction_versions;
ALTER TABLE analysis_instructions DROP CONSTRAINT IF EXISTS chk_instruction_purge_state;
ALTER TABLE analysis_instructions DROP COLUMN IF EXISTS purge_after, DROP COLUMN IF EXISTS purge_state,
    DROP COLUMN IF EXISTS deletion_reason, DROP COLUMN IF EXISTS deleted_by_user_uuid, DROP COLUMN IF EXISTS deleted_at;
DROP TABLE IF EXISTS retention_audit_events;
DROP TABLE IF EXISTS retention_call_deletions;
DROP TRIGGER IF EXISTS subscriptions_extend_call_retention ON subscriptions;
DROP FUNCTION IF EXISTS extend_calls_after_plan_upgrade();
DROP TRIGGER IF EXISTS calls_set_retention ON calls;
DROP FUNCTION IF EXISTS set_call_retention();
DROP INDEX IF EXISTS idx_calls_retention_due;
ALTER TABLE calls DROP CONSTRAINT IF EXISTS chk_calls_retention_hold,
    DROP CONSTRAINT IF EXISTS chk_calls_retention_version,
    DROP CONSTRAINT IF EXISTS chk_calls_retention_state,
    DROP CONSTRAINT IF EXISTS chk_calls_retention_days,
    DROP COLUMN IF EXISTS retention_version, DROP COLUMN IF EXISTS retention_hold_reason,
    DROP COLUMN IF EXISTS retention_hold_until, DROP COLUMN IF EXISTS retention_state,
    DROP COLUMN IF EXISTS retention_expires_at, DROP COLUMN IF EXISTS retention_days_at_creation,
    DROP COLUMN IF EXISTS retention_base_at;
UPDATE plans SET history_retention_days=730,updated_at=now() WHERE code='business_pro';
