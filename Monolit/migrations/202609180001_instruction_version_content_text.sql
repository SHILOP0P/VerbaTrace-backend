-- +goose Up
-- The text of an instruction version is extracted from its file on every
-- analysis. Versions are immutable, so the extracted text is too; keeping it
-- next to the version spares a PDF or DOCX parse per call and gives later steps
-- the exact text the analysis read.
ALTER TABLE analysis_instruction_versions ADD COLUMN content_text TEXT;

-- +goose Down
ALTER TABLE analysis_instruction_versions DROP COLUMN IF EXISTS content_text;
