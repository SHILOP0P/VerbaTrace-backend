-- +goose Up
ALTER TABLE processing_jobs
    ADD COLUMN started_at TIMESTAMPTZ NULL;

-- Existing completed jobs have no reliable execution-start timestamp and are
-- intentionally excluded from the processing-duration metric.

-- +goose Down
ALTER TABLE processing_jobs
    DROP COLUMN IF EXISTS started_at;
