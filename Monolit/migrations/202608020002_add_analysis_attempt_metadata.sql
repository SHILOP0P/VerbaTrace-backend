-- +goose Up
CREATE TABLE call_analysis_attempts (
    analysis_attempt_uuid UUID PRIMARY KEY,
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    transcription_revision INTEGER NOT NULL CHECK (transcription_revision > 0),
    requested_by_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('pending','processing','done','failed','superseded')),
    error_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_call_analysis_attempts_call_created
    ON call_analysis_attempts (call_uuid, created_at DESC);
CREATE UNIQUE INDEX uq_call_analysis_attempt_active
    ON call_analysis_attempts (call_uuid) WHERE status IN ('pending','processing');

ALTER TABLE call_analyses
    ADD COLUMN IF NOT EXISTS transcription_revision INTEGER NULL,
    ADD COLUMN IF NOT EXISTS source_attempt_uuid UUID NULL REFERENCES call_analysis_attempts(analysis_attempt_uuid);

-- +goose Down
ALTER TABLE call_analyses DROP COLUMN IF EXISTS source_attempt_uuid;
ALTER TABLE call_analyses DROP COLUMN IF EXISTS transcription_revision;
DROP TABLE IF EXISTS call_analysis_attempts;
