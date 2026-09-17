-- +goose Up
-- A business plan belongs to the owner and covers several companies, so one
-- company cannot be cut out of it. Handing over ownership is therefore two
-- operations rather than one: give away the single company under the plan, or
-- give away all of them together. The offer records which it is.
ALTER TABLE company_ownership_transfers
    ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'company',
    -- The companies the previous owner asked to stay in as an ordinary member.
    -- Empty means they leave everything they handed over.
    ADD COLUMN IF NOT EXISTS stay_company_uuids UUID[] NOT NULL DEFAULT '{}';

ALTER TABLE company_ownership_transfers
    ALTER COLUMN company_uuid DROP NOT NULL;

ALTER TABLE company_ownership_transfers
    DROP CONSTRAINT IF EXISTS chk_company_ownership_transfer_scope;
ALTER TABLE company_ownership_transfers
    ADD CONSTRAINT chk_company_ownership_transfer_scope
    CHECK (
        (scope = 'company' AND company_uuid IS NOT NULL)
        OR (scope = 'all' AND company_uuid IS NULL)
    );

-- One pending offer per owner, not per company. Two overlapping offers would
-- race for the same plan, and the second one to be accepted would find the
-- companies already gone.
DROP INDEX IF EXISTS uq_company_ownership_transfer_pending;
CREATE UNIQUE INDEX IF NOT EXISTS uq_company_ownership_transfer_pending_owner
    ON company_ownership_transfers (from_user_uuid)
    WHERE status = 'pending';

-- +goose Down
DROP INDEX IF EXISTS uq_company_ownership_transfer_pending_owner;

UPDATE company_ownership_transfers
SET status = 'canceled', decided_at = COALESCE(decided_at, now())
WHERE scope = 'all' AND status = 'pending';

DELETE FROM company_ownership_transfers WHERE company_uuid IS NULL;

ALTER TABLE company_ownership_transfers
    DROP CONSTRAINT IF EXISTS chk_company_ownership_transfer_scope;

ALTER TABLE company_ownership_transfers
    ALTER COLUMN company_uuid SET NOT NULL;

ALTER TABLE company_ownership_transfers
    DROP COLUMN IF EXISTS stay_company_uuids,
    DROP COLUMN IF EXISTS scope;

CREATE UNIQUE INDEX IF NOT EXISTS uq_company_ownership_transfer_pending
    ON company_ownership_transfers (company_uuid)
    WHERE status = 'pending';
