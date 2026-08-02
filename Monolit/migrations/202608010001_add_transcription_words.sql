-- +goose Up
ALTER TABLE call_transcriptions
    ADD COLUMN words JSONB NULL;

-- +goose Down
ALTER TABLE call_transcriptions
    DROP COLUMN IF EXISTS words;
