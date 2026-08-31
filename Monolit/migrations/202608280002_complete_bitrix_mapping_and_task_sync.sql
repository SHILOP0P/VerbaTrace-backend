-- +goose Up
CREATE TABLE integration_mapping_bulk_commands (
    command_uuid UUID PRIMARY KEY,
    connection_uuid UUID NOT NULL REFERENCES integration_connections(connection_uuid) ON DELETE RESTRICT,
    requested_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    idempotency_key_hash BYTEA NOT NULL,
    preview_hash TEXT NOT NULL,
    changes_count INTEGER NOT NULL CHECK (changes_count BETWEEN 1 AND 500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(connection_uuid,requested_by_user_uuid,idempotency_key_hash)
);

ALTER TABLE call_action_external_syncs
    ADD COLUMN external_snapshot JSONB,
    ADD COLUMN review_state TEXT CHECK (review_state IN ('needs_review')),
    ADD COLUMN review_reason TEXT,
    ADD COLUMN last_checked_at TIMESTAMPTZ,
    ADD COLUMN reviewed_by_user_uuid UUID REFERENCES users(user_uuid) ON DELETE RESTRICT,
    ADD COLUMN reviewed_at TIMESTAMPTZ;

CREATE INDEX idx_action_external_sync_reconcile
    ON call_action_external_syncs(available_at,created_at)
    WHERE state='synced' AND review_state IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_action_external_sync_reconcile;
ALTER TABLE call_action_external_syncs
    DROP COLUMN IF EXISTS reviewed_at,
    DROP COLUMN IF EXISTS reviewed_by_user_uuid,
    DROP COLUMN IF EXISTS last_checked_at,
    DROP COLUMN IF EXISTS review_reason,
    DROP COLUMN IF EXISTS review_state,
    DROP COLUMN IF EXISTS external_snapshot;
DROP TABLE IF EXISTS integration_mapping_bulk_commands;
