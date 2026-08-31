-- +goose Up
ALTER TABLE integration_api_keys
    ADD COLUMN permanent_credit_limit BIGINT CHECK (permanent_credit_limit IS NULL OR permanent_credit_limit >= 0),
    ADD COLUMN temporary_credit_limit BIGINT CHECK (temporary_credit_limit IS NULL OR temporary_credit_limit >= 0),
    ADD COLUMN temporary_limit_starts_at TIMESTAMPTZ,
    ADD COLUMN temporary_limit_ends_at TIMESTAMPTZ,
    ADD CONSTRAINT chk_integration_key_temporary_limit CHECK (
        (temporary_credit_limit IS NULL AND temporary_limit_starts_at IS NULL AND temporary_limit_ends_at IS NULL)
        OR
        (temporary_credit_limit IS NOT NULL AND temporary_limit_starts_at IS NOT NULL AND temporary_limit_ends_at IS NOT NULL
         AND temporary_limit_ends_at > temporary_limit_starts_at)
    );

ALTER TABLE ingest_items
    ADD COLUMN key_uuid UUID REFERENCES integration_api_keys(key_uuid) ON DELETE RESTRICT,
    ADD COLUMN source_ref TEXT;

UPDATE ingest_items
SET source_ref = 'vtsrc_' || replace(connection_uuid::text, '-', '') || '_' || encode(convert_to(external_call_id, 'UTF8'), 'hex')
WHERE source_ref IS NULL;

ALTER TABLE ingest_items ALTER COLUMN source_ref SET NOT NULL;
CREATE UNIQUE INDEX uq_ingest_items_source_ref ON ingest_items(source_ref);
CREATE INDEX idx_ingest_items_connection_updated ON ingest_items(connection_uuid, updated_at DESC, ingest_item_uuid DESC);

ALTER TABLE usage_operations
    ADD COLUMN key_uuid UUID REFERENCES integration_api_keys(key_uuid) ON DELETE RESTRICT;

CREATE INDEX idx_usage_operations_key_started ON usage_operations(key_uuid, started_at)
    WHERE key_uuid IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION enqueue_integration_result_webhook() RETURNS trigger AS $$
DECLARE
    item RECORD;
    event_name TEXT;
BEGIN
    IF TG_TABLE_NAME = 'call_transcriptions' AND NEW.status = 'transcribed' AND OLD.status IS DISTINCT FROM NEW.status THEN
        event_name := 'transcription.completed';
    ELSIF TG_TABLE_NAME = 'call_analyses' AND NEW.status = 'done' AND OLD.status IS DISTINCT FROM NEW.status THEN
        event_name := 'analysis.completed';
    ELSE
        RETURN NEW;
    END IF;
    SELECT ingest_item_uuid,application_uuid,connection_uuid,source_ref INTO item
    FROM ingest_items WHERE call_uuid=NEW.call_uuid;
    IF FOUND THEN
        INSERT INTO integration_outbox(outbox_uuid,application_uuid,connection_uuid,event_id,event_type,aggregate_uuid,payload,status)
        VALUES(gen_random_uuid(),item.application_uuid,item.connection_uuid,gen_random_uuid(),event_name,item.ingest_item_uuid,
               jsonb_build_object('ingest_item_uuid',item.ingest_item_uuid,'call_uuid',NEW.call_uuid,'source_ref',item.source_ref,'status','completed'),'pending');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_integration_transcription_completed
AFTER UPDATE OF status ON call_transcriptions FOR EACH ROW EXECUTE FUNCTION enqueue_integration_result_webhook();
CREATE TRIGGER trg_integration_analysis_completed
AFTER UPDATE OF status ON call_analyses FOR EACH ROW EXECUTE FUNCTION enqueue_integration_result_webhook();

-- +goose Down
DROP TRIGGER IF EXISTS trg_integration_analysis_completed ON call_analyses;
DROP TRIGGER IF EXISTS trg_integration_transcription_completed ON call_transcriptions;
DROP FUNCTION IF EXISTS enqueue_integration_result_webhook();
DROP INDEX IF EXISTS idx_usage_operations_key_started;
ALTER TABLE usage_operations DROP COLUMN IF EXISTS key_uuid;
DROP INDEX IF EXISTS idx_ingest_items_connection_updated;
DROP INDEX IF EXISTS uq_ingest_items_source_ref;
ALTER TABLE ingest_items DROP COLUMN IF EXISTS source_ref, DROP COLUMN IF EXISTS key_uuid;
ALTER TABLE integration_api_keys
    DROP CONSTRAINT IF EXISTS chk_integration_key_temporary_limit,
    DROP COLUMN IF EXISTS temporary_limit_ends_at,
    DROP COLUMN IF EXISTS temporary_limit_starts_at,
    DROP COLUMN IF EXISTS temporary_credit_limit,
    DROP COLUMN IF EXISTS permanent_credit_limit;
