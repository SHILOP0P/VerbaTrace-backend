-- +goose Up
ALTER TABLE calls ADD COLUMN transcription_only BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE call_report_exports ALTER COLUMN analysis_uuid DROP NOT NULL;
ALTER TABLE call_report_exports ADD COLUMN content TEXT NOT NULL DEFAULT 'full' CHECK (content IN ('full','transcription'));
ALTER TABLE call_report_exports ADD COLUMN transcription_revision INTEGER NOT NULL DEFAULT 0 CHECK (transcription_revision >= 0);
-- +goose Down
DELETE FROM call_report_exports WHERE analysis_uuid IS NULL;
ALTER TABLE call_report_exports DROP COLUMN transcription_revision, DROP COLUMN content;
ALTER TABLE call_report_exports ALTER COLUMN analysis_uuid SET NOT NULL;
ALTER TABLE calls DROP COLUMN transcription_only;
