-- +goose Up
ALTER TABLE companies
    ADD COLUMN credit_usage_visible_to_members BOOLEAN NOT NULL DEFAULT TRUE;

-- +goose Down
ALTER TABLE companies
    DROP COLUMN IF EXISTS credit_usage_visible_to_members;
