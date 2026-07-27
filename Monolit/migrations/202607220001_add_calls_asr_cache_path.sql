-- +goose Up
ALTER TABLE calls
    ADD COLUMN asr_cache_path TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE calls
    DROP COLUMN IF EXISTS asr_cache_path;
