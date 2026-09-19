-- +goose Up
-- One Bitrix24 CRM comment per call and connection: a new analysis or a
-- published QA revision updates the same comment instead of adding another.
CREATE TABLE integration_crm_notes (
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE CASCADE,
    entity_type TEXT,
    entity_id TEXT,
    external_comment_id TEXT,
    analysis_uuid UUID,
    status TEXT NOT NULL CHECK (status IN ('pending','writing','written','skipped','failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ,
    written_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (call_uuid, connection_uuid)
);
CREATE INDEX idx_integration_crm_notes_due ON integration_crm_notes (available_at) WHERE status IN ('pending','writing');

-- +goose Down
DROP TABLE IF EXISTS integration_crm_notes;
