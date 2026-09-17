-- +goose Up
-- A frozen company's portal is told once that its calls stopped being processed.
-- Remembering that the notice went out is what makes "once" true: the freeze can
-- come from the owner, from an administrator lowering the plan, or from the
-- deletion grace period, and a restart must not send the notice again.
ALTER TABLE integration_connections
    ADD COLUMN freeze_notice_sent_at TIMESTAMPTZ NULL;

-- The worker looks for exactly one thing: a connection a freeze paused that has
-- not been told yet.
CREATE INDEX idx_integration_connections_freeze_notice_pending
    ON integration_connections (company_uuid)
    WHERE paused_by_freeze AND freeze_notice_sent_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_integration_connections_freeze_notice_pending;

ALTER TABLE integration_connections
    DROP COLUMN IF EXISTS freeze_notice_sent_at;
