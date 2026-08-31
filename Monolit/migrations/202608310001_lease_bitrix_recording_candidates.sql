-- Prevent multiple connector workers from recovering the same delayed recording.
-- +goose Up
ALTER TABLE bitrix_call_candidates
    ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_bitrix_call_candidates_recovery_lease
    ON bitrix_call_candidates(status, lease_until, last_seen_at, candidate_uuid)
    WHERE status IN ('waiting_for_recording', 'failed');

-- +goose Down
DROP INDEX IF EXISTS idx_bitrix_call_candidates_recovery_lease;
ALTER TABLE bitrix_call_candidates DROP COLUMN IF EXISTS lease_until;
