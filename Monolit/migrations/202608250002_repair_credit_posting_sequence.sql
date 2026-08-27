-- +goose Up
-- Some development and test databases were migrated while account_sequence
-- had no default, making every ledger posting fail at runtime.
CREATE SEQUENCE IF NOT EXISTS credit_ledger_posting_sequence;

ALTER TABLE credit_ledger_postings
    ALTER COLUMN account_sequence
    SET DEFAULT nextval('credit_ledger_posting_sequence');

-- +goose Down
ALTER TABLE credit_ledger_postings
    ALTER COLUMN account_sequence DROP DEFAULT;
