-- +goose Up
-- Password guessing is cheap without a counter, so failed logins and signups
-- are counted per subject in a sliding window. Postgres holds the counters
-- until the move to microservices puts them in Redis.
CREATE TABLE auth_rate_counters (
    scope TEXT NOT NULL CHECK (scope IN ('login_account','login_ip','signup_ip')),
    subject TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    blocked_until TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, subject)
);

CREATE INDEX idx_auth_rate_counters_window ON auth_rate_counters (window_start);

-- +goose Down
DROP TABLE IF EXISTS auth_rate_counters;
