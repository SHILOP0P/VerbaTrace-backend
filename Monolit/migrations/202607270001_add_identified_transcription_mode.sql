-- +goose Up
ALTER TABLE processing_jobs
    DROP CONSTRAINT chk_processing_jobs_transcription_mode,
    ADD CONSTRAINT chk_processing_jobs_transcription_mode
        CHECK (transcription_mode IN ('standard', 'diarized', 'identified'));

-- +goose Down
ALTER TABLE processing_jobs
    DROP CONSTRAINT chk_processing_jobs_transcription_mode,
    ADD CONSTRAINT chk_processing_jobs_transcription_mode
        CHECK (transcription_mode IN ('standard', 'diarized'));
