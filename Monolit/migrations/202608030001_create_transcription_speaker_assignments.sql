-- +goose Up
CREATE TABLE call_transcription_speaker_assignments (
    call_uuid UUID NOT NULL REFERENCES calls(call_uuid) ON DELETE CASCADE,
    speaker_key VARCHAR(100) NOT NULL,
    display_name VARCHAR(100) NOT NULL DEFAULT '',
    role VARCHAR(32) NOT NULL DEFAULT 'unknown',
    contact_user_uuid UUID REFERENCES users(user_uuid) ON DELETE SET NULL,
    updated_by_user_uuid UUID NOT NULL REFERENCES users(user_uuid) ON DELETE RESTRICT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (call_uuid, speaker_key),
    CONSTRAINT call_transcription_speaker_role_valid CHECK (role IN ('unknown', 'client', 'manager', 'operator', 'partner', 'other'))
);

CREATE INDEX call_transcription_speaker_assignments_contact_idx
    ON call_transcription_speaker_assignments (contact_user_uuid)
    WHERE contact_user_uuid IS NOT NULL;

-- +goose Down
DROP TABLE call_transcription_speaker_assignments;
