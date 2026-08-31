-- +goose Up
CREATE TABLE integration_backfill_candidates (
    backfill_uuid UUID NOT NULL REFERENCES integration_backfills(backfill_uuid) ON DELETE CASCADE,
    candidate_uuid UUID NOT NULL REFERENCES bitrix_call_candidates(candidate_uuid) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(backfill_uuid,candidate_uuid)
);
CREATE INDEX idx_integration_backfill_candidates_candidate
    ON integration_backfill_candidates(candidate_uuid);

-- +goose Down
DROP TABLE IF EXISTS integration_backfill_candidates;
