-- +goose Up
-- Deleting a company and freezing one after a downgrade both end in the same
-- state, and the owner could undo a deletion with the ordinary "activate"
-- button without ever meaning to. The reason is recorded so the two can be told
-- apart, and so undoing a deletion has to be said out loud.
ALTER TABLE companies
    ADD COLUMN IF NOT EXISTS freeze_reason TEXT NULL
        CHECK (freeze_reason IS NULL OR freeze_reason IN ('downgrade', 'deletion'));

UPDATE companies SET freeze_reason = 'downgrade'
WHERE lifecycle_state IN ('frozen', 'soft_deleted') AND freeze_reason IS NULL;

-- +goose Down
ALTER TABLE companies DROP COLUMN IF EXISTS freeze_reason;
