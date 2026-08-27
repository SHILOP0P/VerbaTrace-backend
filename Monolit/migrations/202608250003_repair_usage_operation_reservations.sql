-- +goose Up
CREATE TABLE IF NOT EXISTS usage_operation_reservations (
    usage_operation_reservation_uuid UUID PRIMARY KEY,
    usage_operation_uuid UUID NOT NULL REFERENCES usage_operations(usage_operation_uuid) ON DELETE RESTRICT,
    credit_grant_uuid UUID NOT NULL REFERENCES credit_grants(credit_grant_uuid) ON DELETE RESTRICT,
    reserved_credits BIGINT NOT NULL CHECK (reserved_credits > 0),
    settled_credits BIGINT NOT NULL DEFAULT 0 CHECK (settled_credits >= 0),
    released_credits BIGINT NOT NULL DEFAULT 0 CHECK (released_credits >= 0),
    spend_order INTEGER NOT NULL CHECK (spend_order > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (usage_operation_uuid, credit_grant_uuid),
    UNIQUE (usage_operation_uuid, spend_order),
    CONSTRAINT chk_usage_reservation_allocation
        CHECK (settled_credits + released_credits <= reserved_credits)
);

CREATE INDEX IF NOT EXISTS idx_usage_reservations_operation
    ON usage_operation_reservations(usage_operation_uuid, spend_order);

-- +goose Down
-- This migration repairs databases where the canonical billing migration was
-- recorded before this table was added. Rolling it back must not remove a
-- canonical billing table from databases that already had the complete schema.
SELECT 1;
