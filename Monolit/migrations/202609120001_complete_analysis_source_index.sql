-- +goose Up
ALTER TABLE call_search_chunks
    ADD COLUMN IF NOT EXISTS source_kind TEXT NOT NULL DEFAULT 'transcription';
ALTER TABLE call_search_chunks
    DROP CONSTRAINT IF EXISTS call_search_chunks_source_kind_check;
ALTER TABLE call_search_chunks
    ADD CONSTRAINT call_search_chunks_source_kind_check
    CHECK (source_kind IN ('transcription','analysis'));

ALTER TABLE assistant_citations
    ADD COLUMN IF NOT EXISTS source_kind TEXT NOT NULL DEFAULT 'transcription';
ALTER TABLE assistant_citations
    DROP CONSTRAINT IF EXISTS assistant_citations_source_kind_check;
ALTER TABLE assistant_citations
    ADD CONSTRAINT assistant_citations_source_kind_check
    CHECK (source_kind IN ('transcription','analysis'));

-- Existing ready documents predate analysis-source indexing. Mark them stale so
-- the worker rebuilds each active transcription with both source types.
UPDATE call_search_documents SET status='stale',updated_at=now() WHERE status='ready';

-- +goose Down
ALTER TABLE assistant_citations DROP COLUMN IF EXISTS source_kind;
ALTER TABLE call_search_chunks DROP COLUMN IF EXISTS source_kind;
