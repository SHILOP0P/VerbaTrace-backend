-- +goose Up
-- Moving calls and instruction folders between an owner's own companies is an
-- explicit operation, and it has to leave a trace the owner can read later: it
-- changes who can reach a recording. The admin audit trail cannot hold it, since
-- that one only accepts platform roles as the actor.
--
-- The record deliberately has no append-only trigger. Both company references
-- are ON DELETE SET NULL so that purging a company still works, and a trigger
-- that refused updates would refuse exactly that — which is how an earlier
-- append-only table brought the company purge to a halt.
CREATE TABLE IF NOT EXISTS company_data_transfers (
    transfer_uuid UUID PRIMARY KEY,
    owner_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE CASCADE,
    source_company_uuid UUID NULL REFERENCES companies(company_uuid) ON DELETE SET NULL,
    target_company_uuid UUID NULL REFERENCES companies(company_uuid) ON DELETE SET NULL,
    calls_moved INTEGER NOT NULL DEFAULT 0,
    folders_moved INTEGER NOT NULL DEFAULT 0,
    selected_calls INTEGER NOT NULL DEFAULT 0,
    reason TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_company_data_transfers_counts
        CHECK (calls_moved >= 0 AND folders_moved >= 0 AND selected_calls >= 0)
);

CREATE INDEX IF NOT EXISTS idx_company_data_transfers_owner
    ON company_data_transfers (owner_user_uuid, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_company_data_transfers_target
    ON company_data_transfers (target_company_uuid, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS company_data_transfers;
