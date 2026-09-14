-- +goose Up
ALTER TABLE call_search_chunks
    ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'transcription'
    CHECK (source_kind IN ('transcription','analysis'));

ALTER TABLE assistant_citations
    ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'transcription'
    CHECK (source_kind IN ('transcription','analysis'));

-- +goose Down
ALTER TABLE assistant_citations DROP COLUMN IF EXISTS source_kind;
ALTER TABLE call_search_chunks DROP COLUMN IF EXISTS source_kind;
