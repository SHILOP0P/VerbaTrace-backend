-- +goose Up
CREATE TABLE call_transcription_revision_state (
    transcription_uuid UUID PRIMARY KEY
        REFERENCES call_transcriptions(transcription_uuid) ON DELETE CASCADE,
    active_revision INTEGER NOT NULL CHECK (active_revision > 0),
    updated_by_user_uuid UUID NULL REFERENCES users(user_uuid) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (transcription_uuid, active_revision)
        REFERENCES call_transcription_revisions(transcription_uuid, revision)
        ON DELETE RESTRICT
);

INSERT INTO call_transcription_revision_state (
    transcription_uuid, active_revision, updated_at
)
SELECT transcription_uuid, max(revision), now()
FROM call_transcription_revisions
GROUP BY transcription_uuid;

-- +goose Down
DROP TABLE IF EXISTS call_transcription_revision_state;
