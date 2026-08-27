-- +goose Up
ALTER TABLE ingest_items
    ADD COLUMN ai_mode TEXT NOT NULL DEFAULT 'real' CHECK (ai_mode IN ('mock','real')),
    ADD COLUMN billing_environment TEXT NOT NULL DEFAULT 'production' CHECK (billing_environment IN ('sandbox','production'));

-- +goose Down
ALTER TABLE ingest_items DROP COLUMN IF EXISTS billing_environment;
ALTER TABLE ingest_items DROP COLUMN IF EXISTS ai_mode;
