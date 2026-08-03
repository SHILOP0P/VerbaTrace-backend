-- +goose Up
ALTER TABLE call_transcription_speaker_assignments
    ADD COLUMN custom_role VARCHAR(100) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE call_transcription_speaker_assignments
    DROP COLUMN custom_role;
