-- +goose Up
-- A frozen company must not keep importing calls, but the connection has to come
-- back exactly as it was once the company is switched on again. A connection an
-- owner paused by hand and one the freeze paused look identical without this
-- flag, and resuming would silently undo the owner's decision.
ALTER TABLE integration_connections
    ADD COLUMN IF NOT EXISTS paused_by_freeze BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_integration_connections_paused_by_freeze
    ON integration_connections (company_uuid)
    WHERE paused_by_freeze;

-- +goose Down
DROP INDEX IF EXISTS idx_integration_connections_paused_by_freeze;

ALTER TABLE integration_connections
    DROP COLUMN IF EXISTS paused_by_freeze;
