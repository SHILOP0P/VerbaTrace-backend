-- +goose Up
ALTER TABLE call_action_dispositions DROP COLUMN IF EXISTS client_request_key;

-- +goose Down
ALTER TABLE call_action_dispositions
    ADD COLUMN IF NOT EXISTS client_request_key TEXT NULL
    CHECK (client_request_key IS NULL OR char_length(client_request_key) BETWEEN 8 AND 200);
